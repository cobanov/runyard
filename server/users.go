package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const usersUsage = `usage (users and teams matter when RUNYARD_AUTH=1):
  runyard-server user add [-admin] NAME     prints a generated password
  runyard-server user passwd NAME           prints a new password, logs the user out everywhere
  runyard-server user ls | rm NAME
  runyard-server team add NAME | rm NAME | ls
  runyard-server team join TEAM USER | leave TEAM USER
  runyard-server project team PROJECT TEAM  only TEAM's members (and admins) see PROJECT; - for everyone
  runyard-server key create [-user USER] NAME  a key that acts as USER; without -user it sees everything`

func password() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// exec runs one statement that must change exactly one row, so a typo in a name is an error, not a silent no-op.
func exec(ctx context.Context, db *pgxpool.Pool, what, sql string, args ...any) error {
	tag, err := db.Exec(ctx, sql, args...)
	if err == nil && tag.RowsAffected() == 0 {
		return noSuch(what)
	}
	return err
}

// noSuch is a missing row: "no such channel" on the command line, a 404 from the API.
type noSuch string

func (n noSuch) Error() string      { return "no such " + string(n) }
func (noSuch) Is(target error) bool { return target == pgx.ErrNoRows }

func userCmd(ctx context.Context, db *pgxpool.Pool, args []string) error {
	if len(args) == 0 {
		return errors.New(usersUsage)
	}
	switch {
	case args[0] == "add":
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		admin := fs.Bool("admin", false, "sees every project")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 1 {
			return errors.New(usersUsage)
		}
		pw := password()
		if _, err := db.Exec(ctx, `INSERT INTO users (name, password, admin) VALUES ($1, $2, $3)`, fs.Arg(0), hashPassword(pw), *admin); err != nil {
			return err
		}
		fmt.Printf("user %s, password %s\n", fs.Arg(0), pw)
	case args[0] == "passwd" && len(args) == 2:
		pw := password()
		if err := exec(ctx, db, "user", `UPDATE users SET password = $2 WHERE name = $1`, args[1], hashPassword(pw)); err != nil {
			return err
		}
		db.Exec(ctx, `DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE name = $1)`, args[1])
		fmt.Printf("user %s, password %s\n", args[1], pw)
	case args[0] == "rm" && len(args) == 2:
		return exec(ctx, db, "user", `DELETE FROM users WHERE name = $1`, args[1])
	case args[0] == "ls":
		rows, _ := db.Query(ctx, `SELECT u.name, u.admin, coalesce(string_agg(t.name, ',' ORDER BY t.name), '') FROM users u
			LEFT JOIN members m ON m.user_id = u.id LEFT JOIN teams t ON t.id = m.team_id GROUP BY u.id ORDER BY u.name`)
		var name, teams string
		var admin bool
		_, err := pgx.ForEachRow(rows, []any{&name, &admin, &teams}, func() error {
			role := "user"
			if admin {
				role = "admin"
			}
			fmt.Printf("%s\t%s\tteams=%s\n", name, role, teams)
			return nil
		})
		return err
	default:
		return errors.New(usersUsage)
	}
	return nil
}

func teamCmd(ctx context.Context, db *pgxpool.Pool, args []string) error {
	switch {
	case len(args) == 2 && args[0] == "add":
		_, err := db.Exec(ctx, `INSERT INTO teams (name) VALUES ($1)`, args[1])
		return err
	case len(args) == 2 && args[0] == "rm":
		err := exec(ctx, db, "team", `DELETE FROM teams WHERE name = $1`, args[1])
		if errStatus(err) == 400 { // projects still point at it
			return errors.New("the team still has projects: move them with `project team PROJECT OTHER` (or -) first")
		}
		return err
	case len(args) == 3 && args[0] == "join":
		return exec(ctx, db, "team or user", `INSERT INTO members (team_id, user_id) SELECT t.id, u.id FROM teams t, users u
			WHERE t.name = $1 AND u.name = $2 ON CONFLICT DO NOTHING`, args[1], args[2])
	case len(args) == 3 && args[0] == "leave":
		return exec(ctx, db, "membership", `DELETE FROM members WHERE team_id = (SELECT id FROM teams WHERE name = $1)
			AND user_id = (SELECT id FROM users WHERE name = $2)`, args[1], args[2])
	case len(args) == 1 && args[0] == "ls":
		rows, _ := db.Query(ctx, `SELECT t.name, coalesce((SELECT string_agg(u.name, ',' ORDER BY u.name) FROM members m JOIN users u ON u.id = m.user_id WHERE m.team_id = t.id), ''),
			coalesce((SELECT string_agg(p.name, ',' ORDER BY p.name) FROM projects p WHERE p.team_id = t.id), '') FROM teams t ORDER BY t.name`)
		var name, users, projects string
		_, err := pgx.ForEachRow(rows, []any{&name, &users, &projects}, func() error {
			fmt.Printf("%s\tmembers=%s\tprojects=%s\n", name, users, projects)
			return nil
		})
		return err
	}
	return errors.New(usersUsage)
}

func projectCmd(ctx context.Context, db *pgxpool.Pool, args []string) error {
	if len(args) != 3 || args[0] != "team" {
		return errors.New(usersUsage)
	}
	if args[2] == "-" {
		return exec(ctx, db, "project", `UPDATE projects SET team_id = NULL WHERE name = $1`, args[1])
	}
	return exec(ctx, db, "project or team", `UPDATE projects SET team_id = t.id FROM teams t WHERE projects.name = $1 AND t.name = $2`, args[1], args[2])
}

// keyCmd: key create [-user USER] NAME.
func keyCmd(ctx context.Context, db *pgxpool.Pool, args []string) error {
	fs := flag.NewFlagSet("key", flag.ContinueOnError)
	user := fs.String("user", "", "the key acts as this user")
	if len(args) == 0 || args[0] != "create" || fs.Parse(args[1:]) != nil || fs.NArg() != 1 {
		return errors.New(usersUsage)
	}
	key, err := createKey(ctx, db, fs.Arg(0), strings.TrimSpace(*user))
	if err != nil {
		return err
	}
	fmt.Println(key)
	return nil
}

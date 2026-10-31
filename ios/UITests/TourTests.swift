import XCTest

/// Walks the app against a real server and keeps a screenshot of every screen.
/// RUNYARD_TEST_SERVER (host[:port] or URL) is required; RUNYARD_TEST_USER and RUNYARD_TEST_PASSWORD when the
/// server has RUNYARD_AUTH on. From xcodebuild, pass them as TEST_RUNNER_RUNYARD_TEST_SERVER and so on.
final class TourTests: XCTestCase {
    @MainActor
    func testTour() throws {
        let env = ProcessInfo.processInfo.environment
        let server = try XCTUnwrap(env["RUNYARD_TEST_SERVER"], "set TEST_RUNNER_RUNYARD_TEST_SERVER")
        let app = XCUIApplication()
        app.launchArguments = ["-resetState", "YES"]
        app.launch()

        let field = app.textFields["server"]
        XCTAssert(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText(server)
        shot(app, "01-connect")
        app.buttons["Connect"].tap()

        if let user = env["RUNYARD_TEST_USER"] {
            let name = app.textFields["name"]
            XCTAssert(name.waitForExistence(timeout: 15), "no login form")
            name.tap()
            name.typeText(user)
            let password = app.secureTextFields["password"]
            password.tap()
            password.typeText(env["RUNYARD_TEST_PASSWORD"] ?? "")
            shot(app, "02-login")
            app.buttons["Sign in"].tap()
        }

        XCTAssert(app.navigationBars["Projects"].waitForExistence(timeout: 15), "no project list")
        if env["RUNYARD_TEST_USER"] != nil {
            // iOS offers to save the password, sometimes seconds later; that sheet would swallow the next taps.
            let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
            for _ in 0..<10 {
                if let b = [app.buttons["Not Now"], springboard.buttons["Not Now"]].first(where: \.exists) { b.tap(); break }
                sleep(1)
            }
        }
        sleep(2)
        shot(app, "03-projects")

        // The last match: a running run's card above the projects shows its project's name too.
        let project = env["RUNYARD_TEST_PROJECT"].map { app.buttons.containing(.staticText, identifier: $0).allElementsBoundByIndex.last! }
        (project ?? app.collectionViews.buttons.element(boundBy: app.collectionViews.buttons.count - 1)).tap()
        sleep(2)
        shot(app, "04-runs")
        (env["RUNYARD_TEST_RUN"].map { app.buttons.containing(.staticText, identifier: $0).firstMatch }
            ?? app.collectionViews.buttons.firstMatch).tap()
        XCTAssert(app.segmentedControls.firstMatch.waitForExistence(timeout: 15), "no run page")
        sleep(3)
        shot(app, "05-run-charts")
        for section in ["System", "Samples", "Logs", "Config", "Files"] where app.segmentedControls.buttons[section].exists {
            app.segmentedControls.buttons[section].tap()
            sleep(3)
            shot(app, "06-run-\(section.lowercased())")
        }
        app.tabBars.buttons["Alerts"].tap()
        sleep(2)
        shot(app, "07-alerts")
        app.tabBars.buttons["Server"].tap()
        shot(app, "08-server")
    }

    @MainActor
    private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }
}

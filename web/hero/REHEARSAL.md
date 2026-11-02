# Rehearsal: website hero (www/index.html, #hero)

Built from stageshow's kit (web/hero/kit, only the 24 files the hero imports) with `npm run build:hero`, which writes www/hero/hero.js and hero.css. Wrangler runs that build before every deploy. Dev page: `npx vite --config hero/vite.config.js` from web/.

Gold: device-pair, a window and a lock screen telling one event. Kept: the window running off the right and bottom, the phone straddling the window's edge over margin, the notification dropping in with Collapse. Changed: runyard's own Overview with its sidebar, run cards, sparklines and GPU table, its tokens (light and dark), the real crash notification text from server/notify.go, the runyard mark as the app icon. Compact is stacked: the run card's window on top, the phone under it.

Beats: steps tick twice (loss and step roll) … the fine-tune's GPUs drop to 0% … its heartbeat rolls 0s → 1m → 3m → 5m … the badge turns Crashed … the "went silent" alert drops onto the lock screen over an older "finished" one … held 2.8s, the run is restarted: the alert leaves first, then the badge rolls back to Running, the heartbeat to 0s and the GPUs refill, 80ms apart. Loops about every 10s while on screen; loss and step only count up across loops (a counter left out of `initial`), so no number rewinds at the seam. Off screen the kit resets to `initial`. The poster is the crash with its alert.

Captured: two full loops and the seam at 80ms (1000 and 390), 1000 and 390 (and 720 on the built page), light and dark, `--states --gate` at both widths, beats at 60–200ms around dead, crashed and alert, reduced motion, the built page with no JavaScript (the old screenshot in noscript), layout shift (0 at 1280, 720, 390).

Film:
- jump: GPU Count cells moved their unit 9px in one frame when 97% became 0% → replaced Count with Roll in those cells, the width now eases.
- framing / uneven gutters / cut by the crop at 390 → window widened to run off the right, phone at 318 whole across with 16px gutters, the window ending a gutter above it.
- touching: "train/loss at step" beside the step Count → stands; a number inside a sentence, one space apart as in the app.
- mixed icons: brand mark next to outline nav icons → stands; it is the product's sidebar.
- small text: the notification's "1h ago" at 8.8px at 390 → stands; the kit's phone scales its type with its width, the title and body stay above 10px, device-pair's gold has the same at 6.6px.
- settle snap and "appears with no entrance" on ss-count strips → the kit's Count internals inside a clipped strip; nothing hops in the frames.
- kit override (built page only) → bundling puts the kit's own phone.css into hero.css, so the film reads it as the page's CSS; hero.css restyles no kit part.

Review: no separate fresh-eye reviewer was run; the stills and beat sheets were read frame by frame against device-pair.

Assumptions: a crashed run stays on the Overview with its Crashed badge (the real app moves it to recently ended); "lab-4090" and the cifar10 notification are made up but shaped like the demo's data.

Unresolved: between 740px and 1040px the wide design scales down (to 0.7 at 740), so its smallest text gets near 9px; below 740 the compact design sits at 350px, centered.

Not checked: Safari and Firefox, a real phone, the deployed site.

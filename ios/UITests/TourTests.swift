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

        XCTAssert(app.navigationBars["Overview"].waitForExistence(timeout: 15), "no home screen")
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

        // The project rows sit below the agent and running cards, and the list only builds what is on screen.
        app.collectionViews.firstMatch.swipeUp(velocity: .fast)
        app.collectionViews.firstMatch.swipeUp(velocity: .fast)
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
        for section in ["Samples", "Logs", "System", "Config", "Files"] where app.segmentedControls.buttons[section].exists {
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

    /// The built-in demo, no server needed: what App Review sees.
    @MainActor
    func testDemo() throws {
        let app = XCUIApplication()
        app.launchArguments = ["-resetState", "YES"]
        app.launch()
        let demo = app.buttons["Try the demo"]
        XCTAssert(demo.waitForExistence(timeout: 10))
        shot(app, "01-connect")
        demo.tap()
        XCTAssert(app.navigationBars["Overview"].waitForExistence(timeout: 15), "no home screen")
        sleep(2)
        XCTAssert(app.staticTexts["Running"].exists, "no overview tiles")
        shot(app, "02-home")
        app.buttons.containing(.staticText, identifier: "qwen3-8b-full-lr2e-5").firstMatch.tap()
        XCTAssert(app.segmentedControls.firstMatch.waitForExistence(timeout: 15), "no run page")
        sleep(2)
        shot(app, "03-running-run")
        app.navigationBars.buttons.firstMatch.tap()
        // Machines, agents and the projects sit below the running cards, and the list only builds what is on screen.
        down(app, to: app.staticTexts["Machines"])
        shot(app, "02-home-machines")
        down(app, to: app.staticTexts["Waiting for you"])
        shot(app, "02-home-agents")
        down(app, to: app.staticTexts["Projects"])
        app.collectionViews.firstMatch.swipeUp()
        shot(app, "02-home-projects")
        app.buttons.containing(.staticText, identifier: "qwen-sft").allElementsBoundByIndex.last!.tap()
        let qwen3Row = app.buttons.containing(.staticText, identifier: "qwen3-8b-lora-r16-nopack").firstMatch
        XCTAssert(qwen3Row.waitForExistence(timeout: 10))
        qwen3Row.tap()
        XCTAssert(app.segmentedControls.firstMatch.waitForExistence(timeout: 15), "no run page")
        sleep(2)
        shot(app, "04-crashed-run")
        app.segmentedControls.buttons["Logs"].tap()
        sleep(2)
        shot(app, "05-crashed-logs")
        app.navigationBars.buttons.firstMatch.tap()
        app.navigationBars.buttons.firstMatch.tap()
        down(app, to: app.staticTexts["Projects"])
        app.collectionViews.firstMatch.swipeUp()
        let project = app.buttons.containing(.staticText, identifier: "pixel-diffusion").allElementsBoundByIndex.last!
        XCTAssert(project.waitForExistence(timeout: 10))
        project.tap()
        let ditRow = app.buttons.containing(.staticText, identifier: "dit-s-pixel32").firstMatch
        XCTAssert(ditRow.waitForExistence(timeout: 10))
        ditRow.tap()
        XCTAssert(app.segmentedControls.firstMatch.waitForExistence(timeout: 15), "no run page")
        app.segmentedControls.buttons["Samples"].tap()
        sleep(3)
        shot(app, "06-samples")
        app.tabBars.buttons["Alerts"].tap()
        sleep(2)
        shot(app, "07-alerts")
        app.tabBars.buttons["Server"].tap()
        sleep(2)
        shot(app, "08-server")
        // Last, because it empties the list: dismissed alerts leave it.
        app.tabBars.buttons["Alerts"].tap()
        app.buttons["Dismiss all"].tap()
        XCTAssert(app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Show'")).firstMatch.waitForExistence(timeout: 5), "no way back to the dismissed")
        XCTAssertFalse(app.staticTexts["loss is NaN"].exists, "a dismissed alert stayed in the list")
        shot(app, "09-alerts-dismissed")
    }

    /// Scrolls the list until `element` is on screen.
    @MainActor
    private func down(_ app: XCUIApplication, to element: XCUIElement) {
        for _ in 0..<12 where !element.isHittable { app.collectionViews.firstMatch.swipeUp(velocity: .slow) }
        XCTAssert(element.exists, "not found below: \(element)")
    }

    @MainActor
    private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }
}

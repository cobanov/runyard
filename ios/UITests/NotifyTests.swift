import XCTest

/// A tap on a notification opens its run, with the app in the background and after it was closed.
/// Needs RUNYARD_TEST_SERVER (a server without sign-in) and RUNYARD_TEST_RUN (a run's id), and the host pushing
/// while it runs, every few seconds:
/// `xcrun simctl push booted dev.cobanov.runyard note.json`, {"aps":{"alert":{"title":"tap-test"}},"run":"<id>"}
final class NotifyTests: XCTestCase {
    @MainActor
    func testTapOpensRun() throws {
        let env = ProcessInfo.processInfo.environment
        guard let server = env["RUNYARD_TEST_SERVER"], env["RUNYARD_TEST_RUN"] != nil else { throw XCTSkip("no server, or no run to push") }
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let app = XCUIApplication()
        app.launchArguments = ["-resetState", "YES"]
        app.launch()
        let field = app.textFields["server"]
        XCTAssert(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText(server + "\n")
        XCTAssert(app.navigationBars["Overview"].waitForExistence(timeout: 15), "no home screen")
        app.tabBars.buttons["Server"].tap()
        app.switches.firstMatch.switches.firstMatch.tap()
        let allow = springboard.buttons["Allow"]
        if allow.waitForExistence(timeout: 5) { allow.tap() }
        app.tabBars.buttons["Alerts"].tap() // not the tab a run opens on

        XCUIDevice.shared.press(.home)
        tapBanner(springboard)
        XCTAssert(app.segmentedControls.firstMatch.waitForExistence(timeout: 15), "the tap did not open the run from the background")

        app.terminate()
        tapBanner(springboard)
        XCTAssert(app.segmentedControls.firstMatch.waitForExistence(timeout: 20), "the tap did not open the run from a cold start")
    }

    @MainActor
    private func tapBanner(_ springboard: XCUIApplication) {
        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS 'tap-test'")).firstMatch
        XCTAssert(banner.waitForExistence(timeout: 40), "no notification banner")
        banner.tap()
    }
}

import SwiftUI
import UIKit

// The app's look: iOS's own grouped ground and cards, the running teal as the accent (the asset catalog's
// AccentColor), and the system's green, red and orange for how a run ended.

extension ShapeStyle where Self == Color {
    static var paper: Color { Color(uiColor: .systemGroupedBackground) }
    static var panel: Color { Color(uiColor: .secondarySystemGroupedBackground) }
    static var soft: Color { Color(uiColor: .tertiarySystemGroupedBackground) } // a box inside a card
    static var rule: Color { Color(uiColor: .separator) }
    static var ink: Color { .primary }
    static var muted: Color { .secondary }
    static var running: Color { .accentColor }
    static var finished: Color { .green }
    static var failed: Color { .red }
    static var crashed: Color { .orange }
    static var series: Color { .accentColor } // a chart's line
}

let radius: CGFloat = 20, radiusSmall: CGFloat = 10 // a card; an image or a field

func statusColor(_ status: String) -> Color {
    switch status {
    case "running": .running
    case "finished": .finished
    case "failed": .failed
    case "crashed": .crashed
    default: .muted
    }
}

func agentColor(_ state: String) -> Color {
    switch state {
    case "waiting": .crashed
    case "error": .failed
    case "done": .finished
    case "training", "working": .running
    default: .muted
    }
}

func levelColor(_ level: String) -> Color {
    level == "error" ? .failed : level == "warn" ? .crashed : .blue
}

/// A busy bar turns orange: above 90% a GPU is full, which is what one wants to see at a glance.
func loadColor(_ percent: Double) -> Color { percent > 90 ? .orange : .accentColor }

nonisolated func duration(_ seconds: TimeInterval, units: Int = 2) -> String {
    Duration.seconds(max(0, seconds)).formatted(.units(allowed: [.days, .hours, .minutes, .seconds], width: .narrow, maximumUnitCount: units)
        .locale(Locale(identifier: "en_US")))
}

/// "4m ago", from the phone's clock.
func ago(_ date: Date) -> String { duration(Date.now.timeIntervalSince(date), units: 1) + " ago" }

/// A state's dot. The running one breathes; nothing else in the app moves on its own.
struct Dot: View {
    let color: Color
    var breathes = false
    @Environment(\.accessibilityReduceMotion) private var still
    @State private var dim = false

    var body: some View {
        Circle().fill(color).frame(width: 8, height: 8)
            .opacity(dim ? 0.35 : 1)
            .onAppear {
                guard breathes, !still else { return }
                withAnimation(.easeInOut(duration: 1.2).repeatForever()) { dim = true }
            }
            .alignmentGuide(.firstTextBaseline) { $0[.bottom] + 1.5 } // beside a name, level with its letters
            .accessibilityHidden(true)
    }
}

/// A bar on a soft track: a run's progress in its state colour, a machine's load in teal or orange.
struct Bar: View {
    let value: Double // 0...1
    var fill: Color = .accentColor

    var body: some View {
        Capsule().fill(Color(uiColor: .systemFill)).frame(height: 5)
            .overlay(alignment: .leading) {
                GeometryReader { g in Capsule().fill(fill).frame(width: g.size.width * min(max(value, 0), 1)) }
            }
            .accessibilityHidden(true)
    }
}

/// A piece of a panel behind a list row: rows of one group share an edge and a hairline between them.
private struct PanelPiece: View {
    let top, bottom: Bool

    var body: some View {
        UnevenRoundedRectangle(topLeadingRadius: top ? radius : 0, bottomLeadingRadius: bottom ? radius : 0,
                               bottomTrailingRadius: bottom ? radius : 0, topTrailingRadius: top ? radius : 0, style: .continuous)
            .fill(.panel)
            .overlay(alignment: .bottom) { if !bottom { Color.rule.frame(height: 0.5).padding(.leading, 16) } }
            .padding(.horizontal, 16)
    }
}

extension View {
    /// A list row as row `i` of a panel of `n`; alone, a card. The row keeps the list's swipe and navigation.
    func panelRow(_ i: Int = 0, of n: Int = 1, above: CGFloat = 0, below: CGFloat = 0) -> some View {
        listRowInsets(EdgeInsets(top: 13 + above, leading: 32, bottom: 13 + below, trailing: 32))
            .listRowSeparator(.hidden)
            .listRowBackground(PanelPiece(top: i == 0, bottom: i == n - 1).padding(.top, above).padding(.bottom, below))
    }

    /// A list row on the ground, with no panel: a summary line, a footer.
    func bareRow(above: CGFloat = 0, below: CGFloat = 0) -> some View {
        listRowInsets(EdgeInsets(top: above, leading: 16, bottom: below, trailing: 16))
            .listRowSeparator(.hidden)
            .listRowBackground(Color.clear)
    }

    /// The ground under a list whose rows draw their own panels.
    func paperList() -> some View {
        listStyle(.plain).scrollContentBackground(.hidden).background(.paper).environment(\.defaultMinListRowHeight, 1)
    }

    /// A panel outside a list.
    func card(_ padding: CGFloat = 16) -> some View {
        self.padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.panel, in: .rect(cornerRadius: radius, style: .continuous))
    }
}

/// A section's heading, as a grouped list writes it.
struct Heading: View {
    let text: String
    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text).font(.headline).foregroundStyle(.muted).padding(.leading, 16).accessibilityAddTraits(.isHeader).bareRow(above: 26, below: 8)
    }
}

/// An empty section: one sentence that says what would put something there.
struct Empty: View {
    let text: String
    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text).font(.subheadline).foregroundStyle(.muted).card()
    }
}

/// The one filled button of a screen, in the accent.
struct Primary: ButtonStyle {
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold)).foregroundStyle(.white)
            .frame(maxWidth: .infinity, minHeight: 50)
            .background(Color.accentColor.opacity(!enabled ? 0.35 : configuration.isPressed ? 0.8 : 1), in: .rect(cornerRadius: 14, style: .continuous))
    }
}

/// An input: a card of its own, tall enough for a finger.
struct Field: ViewModifier {
    func body(content: Content) -> some View {
        content.padding(.horizontal, 16).frame(minHeight: 50)
            .background(.panel, in: .rect(cornerRadius: 14, style: .continuous))
    }
}

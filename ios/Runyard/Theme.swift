import SwiftUI
import UIKit

// DESIGN.md's tokens and the few pieces every screen is built from. Colour means state: teal, red and amber
// never mark a link, a button or a tab (the accent colour in the asset catalog is ink for that reason).

extension ShapeStyle where Self == Color {
    static var paper: Color { Color(0xf9fafb, 0x09090b) }
    static var panel: Color { Color(0xffffff, 0x121214) }
    static var soft: Color { Color(0xf3f4f6, 0x1c1c1f) }
    static var rule: Color { Color(0xe5e7eb, 0x27272a) }
    static var ink: Color { Color(0x18181b, 0xfafafa) }
    static var muted: Color { Color(0x6b7280, 0xa1a1aa) }
    static var running: Color { Color(0x0f766e, 0x2dd4bf) }
    static var finished: Color { Color(0x52525b, 0xa1a1aa) }
    static var failed: Color { Color(0xdc2626, 0xf87171) }
    static var crashed: Color { Color(0xb45309, 0xfbbf24) }
    static var series: Color { Color(0x2563eb, 0x60a5fa) } // --c1: a chart's line
}

private extension Color {
    init(_ light: UInt32, _ dark: UInt32) {
        self.init(uiColor: UIColor { t in
            let v = t.userInterfaceStyle == .dark ? dark : light
            return UIColor(red: CGFloat(v >> 16 & 255) / 255, green: CGFloat(v >> 8 & 255) / 255, blue: CGFloat(v & 255) / 255, alpha: 1)
        })
    }
}

let radius: CGFloat = 8, radiusSmall: CGFloat = 6

func statusColor(_ status: String) -> Color {
    switch status {
    case "running": .running
    case "failed": .failed
    case "crashed": .crashed
    default: .finished // finished is quiet
    }
}

func agentColor(_ state: String) -> Color {
    switch state {
    case "waiting": .crashed
    case "error": .failed
    case "training", "working": .running
    default: .finished
    }
}

func levelColor(_ level: String) -> Color {
    level == "error" ? .failed : level == "warn" ? .crashed : .finished
}

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

/// A 4 pt bar on a soft track: progress in the run's state colour, a machine's load in muted whatever the value.
struct Bar: View {
    let value: Double // 0...1
    var fill: Color = .muted

    var body: some View {
        Capsule().fill(.soft).frame(height: 4)
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
        let shape = UnevenRoundedRectangle(topLeadingRadius: top ? radius : 0, bottomLeadingRadius: bottom ? radius : 0,
                                           bottomTrailingRadius: bottom ? radius : 0, topTrailingRadius: top ? radius : 0)
        shape.fill(.panel)
            // The edge runs past the row where the panel goes on, and the clip cuts it there.
            .overlay(shape.strokeBorder(.rule).padding(.top, top ? 0 : -4).padding(.bottom, bottom ? 0 : -4))
            .overlay(alignment: .bottom) { if !bottom { Color.rule.frame(height: 1).padding(.horizontal, 14) } }
            .clipped()
            .padding(.horizontal, 16)
    }
}

extension View {
    /// A list row as row `i` of a panel of `n`; alone, a card. The row keeps the list's swipe and navigation.
    func panelRow(_ i: Int = 0, of n: Int = 1, above: CGFloat = 0, below: CGFloat = 0) -> some View {
        listRowInsets(EdgeInsets(top: 12 + above, leading: 30, bottom: 12 + below, trailing: 30))
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
    func card(_ padding: CGFloat = 14) -> some View {
        self.padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.panel, in: .rect(cornerRadius: radius))
            .overlay(RoundedRectangle(cornerRadius: radius).strokeBorder(.rule))
    }
}

/// A section's heading: sentence case, small and muted, 24 below what came before.
struct Heading: View {
    let text: String
    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text).font(.footnote).foregroundStyle(.muted).accessibilityAddTraits(.isHeader).bareRow(above: 24, below: 8)
    }
}

/// An empty section: one sentence that says what would put something there.
struct Empty: View {
    let text: String
    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text).font(.footnote).foregroundStyle(.muted)
            .padding(.horizontal, 14).padding(.vertical, 10)
            .frame(maxWidth: .infinity, minHeight: 38, alignment: .leading)
            .overlay(RoundedRectangle(cornerRadius: radius).strokeBorder(.rule, style: StrokeStyle(lineWidth: 1, dash: [4, 3])))
    }
}

/// The one filled button of a screen: ink on paper.
struct Primary: ButtonStyle {
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.semibold)).foregroundStyle(.paper)
            .frame(maxWidth: .infinity, minHeight: 44)
            .background(Color.ink.opacity(!enabled ? 0.3 : configuration.isPressed ? 0.8 : 1), in: .rect(cornerRadius: radiusSmall))
    }
}

/// An input: a panel inside a hairline, 44 tall for a finger.
struct Field: ViewModifier {
    func body(content: Content) -> some View {
        content.padding(.horizontal, 12).frame(minHeight: 44)
            .background(.panel, in: .rect(cornerRadius: radiusSmall))
            .overlay(RoundedRectangle(cornerRadius: radiusSmall).strokeBorder(.rule))
    }
}

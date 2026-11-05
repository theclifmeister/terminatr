import Foundation

// The themes' colours (T770; flat since T999, the design page's tokens()).
// Each is checked in ThemeTests: every text colour 4.5:1 or more over its
// surfaces, in light and dark, with and without Increase Contrast
// (ThemeColours.checks), and the status dots apart for colour-blind eyes
// (dotDistance).

extension ThemeColours {
    /// Carbon (the flat look's reference window, the default): neutral
    /// greys and one teal. Dark's surfaces, ink and tile are the colours
    /// measured from that window, exactly; the rest of dark, and all of
    /// light, start from the design page's. Tuned for the checks: light's
    /// action (page 0f8a80, 4.2:1 on a pane), light's good and attention
    /// and dark's good (the status dots, ΔE under 12 for colour-blind eyes
    /// beside the teal), dark's idle (page 66686a; an idle pill sets ink on
    /// it).
    public static let carbon = ThemeColours(
        chrome: Swatch(0xf3f4f5, 0x101213),
        gutter: Swatch(0xeceef0, 0x121418),
        pane: Swatch(0xffffff, 0x17181c),
        line: Swatch(0xdcdee1, 0x292b2f),
        lineSoft: Swatch(0xe6e7e9, 0x202224),
        selection: Swatch(0xe3e4e7, 0x262628),
        tab: Swatch(0xdfe1e4, 0x2c2d31),
        focus: Swatch(0x5b6472, 0xa6aebe),
        ink: Swatch(0x17181c, 0xdddfe0),
        muted: Swatch(0x5e6268, 0x8a8c90),
        faint: Swatch(0x8e9297, 0x66686a),
        action: Swatch(0x0a7076, 0x22a89d),
        onAction: Swatch(0xffffff, 0x04211e),
        good: Swatch(0x5a7a00, 0x6fd08a),
        onGood: Swatch(0xffffff, 0x082215),
        idle: Swatch(0x8e9297, 0x5f6164),
        attention: Swatch(0x9c1c1c, 0xe5484d),
        onAttention: Swatch(0xffffff, 0x160302),
        question: Swatch(0x2357c4, 0x76a5ff),
        onQuestion: Swatch(0xffffff, 0x0b1730),
        approval: Swatch(0xa84406, 0xf2a65e),
        onApproval: Swatch(0xffffff, 0x2a1606),
        questionTint: Swatch(0xedf2fc, 0x2357c4, alpha: 1, darkAlpha: 0.18),
        approvalTint: Swatch(0xfcf1e8, 0xa84406, alpha: 1, darkAlpha: 0.18),
        command: Swatch(0x17181c, 0x000000, alpha: 0.05, darkAlpha: 0.25),
        terminal: TerminalColours(
            background: Swatch(0x17181c), foreground: Swatch(0xdddfe0),
            cursor: Swatch(0xdddfe0), selection: Swatch(0xa6aebe, alpha: 0.28),
            ansi: ansi(0x2c2d31, 0xff6b6b, 0x55c88f, 0xe7c35a, 0x76a5ff, 0xc792ea, 0x4fc9be, 0xd6d8da,
                       0x6e7075, 0xff8a8a, 0x7ee2a4, 0xf2d47a, 0x93b8ff, 0xd6aef2, 0x7ad6cf, 0xf5f6f7)
        ),
        increased: IncreasedColours(questionTint: Swatch(0xe8effc, 0x162540), approvalTint: Swatch(0xfdf0e4, 0x3a2412), muted: Swatch(0x3f4349, 0xb6b8bc))
    )

    /// Bloom: deep plum and violet, the first theme, now flat.
    public static let bloom = ThemeColours(
        chrome: Swatch(0xf7e5ff, 0x160f2c),
        gutter: Swatch(0xf5dfff, 0x19122f),
        pane: Swatch(0xfdf9ff, 0x1e1733),
        line: Swatch(0xd3bbde, 0x302943),
        lineSoft: Swatch(0xe0c6eb, 0x241d39),
        selection: Swatch(0xe2c8ed, 0x2a233e),
        tab: Swatch(0xdbc2e6, 0x322c45),
        focus: Swatch(0x726285, 0xaea9bb),
        ink: Swatch(0x1c1433, 0xf6f2ff),
        muted: Swatch(0x4f4670, 0xc9c0ea),
        faint: Swatch(0x8c85a2, 0x8d85aa),
        action: Swatch(0x5a3cf0, 0xa58fff),
        onAction: Swatch(0xffffff, 0x160f33),
        good: Swatch(0x0a7a54, 0x5fe0a8),
        onGood: Swatch(0xffffff, 0x062a1c),
        idle: Swatch(0xa49cc0, 0x6c6390),
        attention: Swatch(0xbb2e18, 0xff8a73),
        onAttention: Swatch(0xffffff, 0x2a0c06),
        question: Swatch(0x5a3cf0, 0xb9a8ff),
        onQuestion: Swatch(0xffffff, 0x160f33),
        approval: Swatch(0xc2410c, 0xff9a6b),
        onApproval: Swatch(0xffffff, 0x2a0c06),
        questionTint: Swatch(0xefeaff, 0x5a3cf0, alpha: 0.95, darkAlpha: 0.28),
        approvalTint: Swatch(0xfff1ec, 0xff6a4a, alpha: 1, darkAlpha: 0.22),
        command: Swatch(0x1c1433, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x1a1430), foreground: Swatch(0xefeaff),
            cursor: Swatch(0xa58fff), selection: Swatch(0x7b5cff, alpha: 0.4),
            ansi: ansi(0x2a2148, 0xff7b7b, 0x9be15d, 0xffd166, 0x8fa8ff, 0xff7ad9, 0x4fe0d0, 0xe6e0f5,
                       0x7a72a0, 0xff9a9a, 0xb8f27c, 0xffe08a, 0xadc0ff, 0xffa3e8, 0x84f0e4, 0xffffff)
        ),
        increased: IncreasedColours(questionTint: Swatch(0xefeaff, 0x2b2160), approvalTint: Swatch(0xfff1ec, 0x3a1a1c), muted: Swatch(0x3a3258, 0xddd6f5))
    )

    /// Calm, easy on the eye: warm paper and charcoal, never pure white
    /// or black; a slate-teal action.
    public static let calm = ThemeColours(
        chrome: Swatch(0xf6f2eb, 0x1c1a17),
        gutter: Swatch(0xf4efe6, 0x1f1d1a),
        pane: Swatch(0xfdfcfb, 0x24221f),
        line: Swatch(0xd2ccc3, 0x353331),
        lineSoft: Swatch(0xded8ce, 0x2a2825),
        selection: Swatch(0xe0dbd0, 0x2f2d2b),
        tab: Swatch(0xd9d4ca, 0x373533),
        focus: Swatch(0x7c776f, 0xa9a399),
        ink: Swatch(0x2e2924, 0xebe4d6),
        muted: Swatch(0x5c5449, 0xb9af9f),
        faint: Swatch(0x948f87, 0x857e72),
        action: Swatch(0x3d5f86, 0x9db8dc),
        onAction: Swatch(0xfbf7ef, 0x141b26),
        good: Swatch(0x52741c, 0xb8d48e),
        onGood: Swatch(0xfbf7ef, 0x1a2210),
        idle: Swatch(0xb0a698, 0x6b645a),
        attention: Swatch(0x9e3446, 0xdb846c),
        onAttention: Swatch(0xfbf7ef, 0x2a140c),
        question: Swatch(0x3d5f86, 0x9db8dc),
        onQuestion: Swatch(0xfbf7ef, 0x141b26),
        approval: Swatch(0x8c4f0c, 0xe1b36e),
        onApproval: Swatch(0xfbf7ef, 0x2a1d08),
        questionTint: Swatch(0xe6ebf2, 0x3d5f86, alpha: 0.95, darkAlpha: 0.26),
        approvalTint: Swatch(0xf7ebda, 0x8c4f0c, alpha: 1, darkAlpha: 0.26),
        command: Swatch(0x2e2924, 0x000000, alpha: 0.06, darkAlpha: 0.25),
        terminal: TerminalColours(
            background: Swatch(0x23211d), foreground: Swatch(0xe6dfd2),
            cursor: Swatch(0x9db8dc), selection: Swatch(0x9db8dc, alpha: 0.28),
            ansi: ansi(0x34302a, 0xe28e72, 0xa9c784, 0xe3c07a, 0x93b6cc, 0xcca3c3, 0x8fc5b5, 0xddd5c6,
                       0x857c6f, 0xeca68e, 0xbdd6a0, 0xeed39a, 0xabc8da, 0xdab8d2, 0xa8d7c9, 0xf4ede0)
        ),
        increased: IncreasedColours(questionTint: Swatch(0xe3e9f1, 0x202c3c), approvalTint: Swatch(0xf6e9d7, 0x382916), muted: Swatch(0x433d34, 0xd6cdbd))
    )

    /// Graphite, professional: neutral greys and one restrained blue.
    public static let graphite = ThemeColours(
        chrome: Swatch(0xf3f4f6, 0x17191d),
        gutter: Swatch(0xf0f1f4, 0x1a1c20),
        pane: Swatch(0xfcfdfd, 0x1f2125),
        line: Swatch(0xcdcfd2, 0x313236),
        lineSoft: Swatch(0xd9dbde, 0x25272b),
        selection: Swatch(0xdbdde0, 0x2b2d30),
        tab: Swatch(0xd4d6d9, 0x333538),
        focus: Swatch(0x6c6e72, 0xa5a7ab),
        ink: Swatch(0x16181d, 0xe8eaee),
        muted: Swatch(0x4a505a, 0xa3a9b3),
        faint: Swatch(0x888d93, 0x757981),
        action: Swatch(0x2357c4, 0x76a5ff),
        onAction: Swatch(0xffffff, 0x0b1730),
        good: Swatch(0x1b7543, 0x55c88f),
        onGood: Swatch(0xffffff, 0x082215),
        idle: Swatch(0x9aa1ab, 0x5d646e),
        attention: Swatch(0xb42318, 0xff8077),
        onAttention: Swatch(0xffffff, 0x2a0a08),
        question: Swatch(0x2357c4, 0x76a5ff),
        onQuestion: Swatch(0xffffff, 0x0b1730),
        approval: Swatch(0xa84406, 0xf2a65e),
        onApproval: Swatch(0xffffff, 0x2a1606),
        questionTint: Swatch(0xedf2fc, 0x2357c4, alpha: 1, darkAlpha: 0.22),
        approvalTint: Swatch(0xfcf1e8, 0xa84406, alpha: 1, darkAlpha: 0.22),
        command: Swatch(0x16181d, 0x000000, alpha: 0.05, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x111317), foreground: Swatch(0xd8dce2),
            cursor: Swatch(0x76a5ff), selection: Swatch(0x2357c4, alpha: 0.45),
            ansi: ansi(0x2a2e35, 0xff6b6b, 0x5fd38d, 0xe7c35a, 0x6ea0ff, 0xc792ea, 0x56c8d8, 0xd6dae0,
                       0x6b7280, 0xff8a8a, 0x7ee2a4, 0xf2d47a, 0x93b8ff, 0xd6aef2, 0x7ad6e3, 0xf5f7fa)
        ),
        increased: IncreasedColours(questionTint: Swatch(0xe8effc, 0x162540), approvalTint: Swatch(0xfdf0e4, 0x3a2412), muted: Swatch(0x2f343c, 0xc9ced6))
    )

    /// Dusk, dimmed for night work: very low light, muted violet and
    /// teal; its light is dimmed as well, lavender-grey with no pure
    /// white.
    public static let dusk = ThemeColours(
        chrome: Swatch(0xe6e3ea, 0x0e0d13),
        gutter: Swatch(0xe0dde5, 0x111016),
        pane: Swatch(0xfaf9fa, 0x16151b),
        line: Swatch(0xbcb8c1, 0x29282d),
        lineSoft: Swatch(0xc7c3cc, 0x1c1c21),
        selection: Swatch(0xc9c5ce, 0x222227),
        tab: Swatch(0xc2bfc8, 0x2b2a2f),
        focus: Swatch(0x6b6773, 0x93909e),
        ink: Swatch(0x221e2b, 0xd2cddf),
        muted: Swatch(0x48425a, 0x9a93ad),
        faint: Swatch(0x868292, 0x6c677a),
        action: Swatch(0x4f3f8c, 0xa596d6),
        onAction: Swatch(0xeceaf1, 0x15121f),
        good: Swatch(0x1d5e5a, 0x86cfc8),
        onGood: Swatch(0xeceaf1, 0x0d1d18),
        idle: Swatch(0x8d879a, 0x4a4558),
        attention: Swatch(0x96402a, 0xe0907a),
        onAttention: Swatch(0xeceaf1, 0x240e0e),
        question: Swatch(0x285e66, 0x8fc0c6),
        onQuestion: Swatch(0xeceaf1, 0x0d1d1f),
        approval: Swatch(0x7c4b14, 0xd5a36f),
        onApproval: Swatch(0xeceaf1, 0x241708),
        questionTint: Swatch(0xdde4e8, 0x285e66, alpha: 0.95, darkAlpha: 0.24),
        approvalTint: Swatch(0xebe0d6, 0x7c4b14, alpha: 1, darkAlpha: 0.24),
        command: Swatch(0x221e2b, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x16141e, 0x111018), foreground: Swatch(0xc9c4d6, 0xbcb7c9),
            cursor: Swatch(0xa596d6), selection: Swatch(0xa596d6, alpha: 0.25),
            ansi: ansi(0x26232f, 0xd98a8a, 0x8cbfa6, 0xd6b47c, 0x93a6d8, 0xb39ad6, 0x84bdc3, 0xc9c4d6,
                       0x6e6884, 0xe6a3a3, 0xa3cfb9, 0xe0c496, 0xa9b9e2, 0xc4b0e0, 0x9ecbd0, 0xe0dcea)
        ),
        increased: IncreasedColours(questionTint: Swatch(0xdde5ea, 0x14262a), approvalTint: Swatch(0xeee3d8, 0x2c2116), muted: Swatch(0x38334a, 0xbdb7cc))
    )

    /// Aurora, vibrant: mint and violet on deep navy; icy by day.
    public static let aurora = ThemeColours(
        chrome: Swatch(0xedf6ff, 0x071230),
        gutter: Swatch(0xe9f4ff, 0x0a1532),
        pane: Swatch(0xfbfdff, 0x101a37),
        line: Swatch(0xc6d2de, 0x222c47),
        lineSoft: Swatch(0xd2deeb, 0x16203c),
        selection: Swatch(0xd4e0ed, 0x1c2642),
        tab: Swatch(0xcdd9e6, 0x252e49),
        focus: Swatch(0x627089, 0xa4adbd),
        ink: Swatch(0x0b1a3a, 0xeef6ff),
        muted: Swatch(0x34446a, 0xb8c8e2),
        faint: Swatch(0x7a859e, 0x7d8ba6),
        action: Swatch(0x005a7a, 0x5ee6d0),
        onAction: Swatch(0xffffff, 0x04201c),
        good: Swatch(0x467a0c, 0xc2f27c),
        onGood: Swatch(0xffffff, 0x1a2606),
        idle: Swatch(0x8fa0bf, 0x50618a),
        attention: Swatch(0xb81f5a, 0xff7aa8),
        onAttention: Swatch(0xffffff, 0x2a0616),
        question: Swatch(0x4636c8, 0xa7b9ff),
        onQuestion: Swatch(0xffffff, 0x0c1238),
        approval: Swatch(0xab4808, 0xffb36b),
        onApproval: Swatch(0xffffff, 0x2a1606),
        questionTint: Swatch(0xecebff, 0x4636c8, alpha: 0.95, darkAlpha: 0.3),
        approvalTint: Swatch(0xfff0e3, 0xff8a3a, alpha: 1, darkAlpha: 0.2),
        command: Swatch(0x0b1a3a, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x08122b), foreground: Swatch(0xe1ecff),
            cursor: Swatch(0x5ee6d0), selection: Swatch(0x3ec8ff, alpha: 0.3),
            ansi: ansi(0x1a2546, 0xff6b8f, 0x5ef2a0, 0xffd76b, 0x6fa8ff, 0xe07bff, 0x4fe6ff, 0xdce8ff,
                       0x63759f, 0xff8faa, 0x8affbd, 0xffe598, 0x94bfff, 0xea9dff, 0x85eeff, 0xffffff)
        ),
        increased: IncreasedColours(questionTint: Swatch(0xebe9ff, 0x1d1a55), approvalTint: Swatch(0xfff0e3, 0x3a2414), muted: Swatch(0x2b3858, 0xcfdcf0))
    )

    /// Sunset, vibrant and warm: raspberry actions, embers in dark.
    public static let sunset = ThemeColours(
        chrome: Swatch(0xffefe4, 0x1e0f14),
        gutter: Swatch(0xffebde, 0x211217),
        pane: Swatch(0xfffcf9, 0x26171c),
        line: Swatch(0xdec8ba, 0x37292e),
        lineSoft: Swatch(0xebd4c5, 0x2c1d22),
        selection: Swatch(0xedd6c7, 0x312328),
        tab: Swatch(0xe6cfc1, 0x392c30),
        focus: Swatch(0x806864, 0xb7a9a6),
        ink: Swatch(0x2b1418, 0xfff1ea),
        muted: Swatch(0x663b42, 0xe2c3ba),
        faint: Swatch(0x9c7f82, 0xa08783),
        action: Swatch(0x951d66, 0xe78ae0),
        onAction: Swatch(0xffffff, 0x2a0a1c),
        good: Swatch(0x2a7034, 0x7fe0b0),
        onGood: Swatch(0xffffff, 0x0c240e),
        idle: Swatch(0xc2a19a, 0x7a5a60),
        attention: Swatch(0xb02e0a, 0xff9e70),
        onAttention: Swatch(0xffffff, 0x2a0e04),
        question: Swatch(0x6f2aa6, 0xd6aaff),
        onQuestion: Swatch(0xffffff, 0x1e0a30),
        approval: Swatch(0x964600, 0xffc66e),
        onApproval: Swatch(0xffffff, 0x2a1a04),
        questionTint: Swatch(0xf4e9ff, 0x6f2aa6, alpha: 0.95, darkAlpha: 0.3),
        approvalTint: Swatch(0xfff0e2, 0xff8a3a, alpha: 1, darkAlpha: 0.2),
        command: Swatch(0x2b1418, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x1f1216), foreground: Swatch(0xfbe9e1),
            cursor: Swatch(0xe78ae0), selection: Swatch(0xff6fa3, alpha: 0.3),
            ansi: ansi(0x33202a, 0xff7a6b, 0xa6e07a, 0xffc861, 0x8fb0ff, 0xff8fc8, 0x6fd8d0, 0xf3e2dc,
                       0x86666e, 0xff9a8c, 0xbdeb98, 0xffd88a, 0xadc4ff, 0xffaad6, 0x94e4de, 0xfff6f2)
        ),
        increased: IncreasedColours(questionTint: Swatch(0xf3e8ff, 0x2f1a40), approvalTint: Swatch(0xfff0e6, 0x3d2010), muted: Swatch(0x4f2c31, 0xf0d6cd))
    )

    /// Solar, high legibility: near-black ink on cream (white on black
    /// in dark), a sun-gold action in dark.
    public static let solar = ThemeColours(
        chrome: Swatch(0xfef9ed, 0x0a0a0a),
        gutter: Swatch(0xfdf8e9, 0x0d0d0d),
        pane: Swatch(0xfffefb, 0x131313),
        line: Swatch(0xdcd6c5, 0x252525),
        lineSoft: Swatch(0xe9e2d1, 0x191919),
        selection: Swatch(0xebe5d3, 0x1f1f1f),
        tab: Swatch(0xe4ddcc, 0x272727),
        focus: Swatch(0x6c6961, 0xb1b1b1),
        ink: Swatch(0x0b0b0b, 0xffffff),
        muted: Swatch(0x38332a, 0xd8d2c2),
        faint: Swatch(0x7e7a73, 0x938f85),
        action: Swatch(0x0045ad, 0xffd23f),
        onAction: Swatch(0xffffff, 0x0a0a0a),
        good: Swatch(0x00653a, 0x5ce8c8),
        onGood: Swatch(0xffffff, 0x04140a),
        idle: Swatch(0x8a8270, 0x6b6658),
        attention: Swatch(0xad0020, 0xff7070),
        onAttention: Swatch(0xffffff, 0x1a0000),
        question: Swatch(0x5428ad, 0xb4cfff),
        onQuestion: Swatch(0xffffff, 0x06122a),
        approval: Swatch(0x854700, 0xffaa4d),
        onApproval: Swatch(0xffffff, 0x1a0e00),
        questionTint: Swatch(0xf0eaff, 0x5428ad, alpha: 1, darkAlpha: 0.3),
        approvalTint: Swatch(0xfff1dc, 0xffaa4d, alpha: 1, darkAlpha: 0.16),
        command: Swatch(0x0b0b0b, 0x000000, alpha: 0.07, darkAlpha: 0.5),
        terminal: TerminalColours(
            background: Swatch(0x0a0a0a), foreground: Swatch(0xf5f5f5),
            cursor: Swatch(0xffd23f), selection: Swatch(0xffd23f, alpha: 0.3),
            ansi: ansi(0x262626, 0xff5f5f, 0x5fe08a, 0xffd23f, 0x6aa8ff, 0xff7ae0, 0x3fe0e0, 0xe8e8e8,
                       0x8a8a8a, 0xff8a8a, 0x8af0a8, 0xffe17a, 0x9cc4ff, 0xff9eea, 0x7aeeee, 0xffffff)
        ),
        increased: IncreasedColours(questionTint: Swatch(0xefe8ff, 0x1f1a33), approvalTint: Swatch(0xfff0dc, 0x2e1d08), muted: Swatch(0x1f1c14, 0xe8e3d6))
    )

    private static func ansi(_ c: UInt32...) -> [Swatch] {
        c.map { Swatch($0) }
    }
}

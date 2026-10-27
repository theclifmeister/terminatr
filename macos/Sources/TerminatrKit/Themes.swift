import Foundation

// The themes' colours (T770). Each is checked in ThemeTests: every text
// colour 4.5:1 or more over its surfaces at their worst spot, in light
// and dark, with and without Increase Contrast (ThemeColours.checks),
// and the status dots apart for colour-blind eyes (dotDistance).

extension ThemeColours {
    /// Bloom (design T744, concept 02): a vivid gradient mesh under
    /// frosted panels, smoked glass in dark. No text sits on the raw
    /// mesh: the sidebar and inspector have their frost, the detail its
    /// glass, the cards theirs over it (the menu bar's panels as well,
    /// since T770: over the bare material alone its dark action read
    /// 2.6:1).
    ///
    /// Contrast (T752), each text pair over its worst spot (checks):
    ///
    ///                                light           dark
    ///     ink on the sidebar         12.9 (violet)   11.1 (lime)
    ///     muted on the sidebar        6.3 (violet)    7.1 (lime)
    ///     action on the sidebar       4.6 (violet)    4.6 (lime)
    ///     ink on the glass           14.0 (violet)   14.5 (lime)
    ///     action on the glass         5.0 (violet)    6.1 (lime)
    ///     attention on the glass      4.8 (violet)    7.0 (lime)
    ///     ink on a card              17.1 (violet)   12.0 (lime)
    ///     muted on a card             8.4 (violet)    7.7 (lime)
    ///     approval on its card        4.7 (coral)     5.6 (coral)
    ///     onPrompt on prompt          4.9             6.1
    ///
    /// So the tones' colours (approval, attention, good) are never text
    /// on the sidebar (3.8–4.4:1 there in light): a tag there is a
    /// filled pill. Against Bloom's page: light approval, attention and
    /// good are darker (Bloom's e2541c, d83a22, 0e9f6e read 3.5–4.3:1 on
    /// the cards), the light sidebar's tint 60% and the dark one smoked
    /// glass, since here the sidebar has no window glass under it; dark
    /// bubblegum, the mesh's centre, is calmer (8c1f6e).
    public static let bloom = ThemeColours(
        mesh: [
            Swatch(0xff6b4a, 0xc2365a), // coral
            Swatch(0x7b5cff, 0x4b2fb8), // violet
            Swatch(0xc6f432, 0x5b7a10), // lime
            Swatch(0x29d3c3, 0x0f6e6a), // lagoon
            Swatch(0xff5fb7, 0x5a1d52), // bubblegum
        ],
        base: Swatch(0xf3d7ff, 0x160f2c),
        ink: Swatch(0x1c1433, 0xf6f2ff),
        muted: Swatch(0x4f4670, 0xc9c0ea),
        line: Swatch(0x1c1433, 0xf6f2ff, alpha: 0.12),
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
        sidebarTint: Swatch(0xffffff, 0x120e24, alpha: 0.60, darkAlpha: 0.40),
        glass: Swatch(0xffffff, 0x120e24, alpha: 0.70, darkAlpha: 0.72),
        bar: Swatch(0xffffff, 0xffffff, alpha: 0.35, darkAlpha: 0.04),
        cardTint: Swatch(0xffffff, 0xffffff, alpha: 0.80, darkAlpha: 0.07),
        edge: Swatch(0xffffff, 0xffffff, alpha: 0.85, darkAlpha: 0.14),
        meter: [Swatch(0x5a3cf0, 0xa58fff), Swatch(0xff5fb7), Swatch(0xff6b4a)],
        prompt: [Swatch(0x5a3cf0), Swatch(0x9b3cf0, 0x8a2fd6)],
        onPrompt: Swatch(0xffffff),
        agentCard: Swatch(0xffffff, 0xffffff, alpha: 0.80, darkAlpha: 0.07),
        questionTint: Swatch(0xefeaff, 0x5a3cf0, alpha: 0.95, darkAlpha: 0.28),
        approvalTint: Swatch(0xfff1ec, 0xff6a4a, alpha: 1, darkAlpha: 0.22),
        command: Swatch(0x1c1433, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x1a1430), foreground: Swatch(0xefeaff),
            cursor: Swatch(0xa58fff), selection: Swatch(0x7b5cff, alpha: 0.4),
            ansi: ansi(0x2a2148, 0xff7b7b, 0x9be15d, 0xffd166, 0x8fa8ff, 0xff7ad9, 0x4fe0d0, 0xe6e0f5,
                       0x7a72a0, 0xff9a9a, 0xb8f27c, 0xffe08a, 0xadc0ff, 0xffa3e8, 0x84f0e4, 0xffffff)
        ),
        opaque: OpaqueColours(
            panel: Swatch(0xffffff, 0x120e24), bar: Swatch(0xf6f2ff, 0x1c1638), card: Swatch(0xffffff, 0x221a40),
            questionTint: Swatch(0xefeaff, 0x2b2160), approvalTint: Swatch(0xfff1ec, 0x3a1a1c),
            edge: Swatch(0x4f4670, 0xc9c0ea), muted: Swatch(0x3a3258, 0xddd6f5)
        ),
        titleFamily: "Bricolage Grotesque",
        monoFamily: "DM Mono"
    )

    /// Calm, easy on the eye: warm paper and charcoal, never pure white
    /// or black; sage, sand and clay in a soft mesh; a slate-teal action.
    public static let calm = ThemeColours(
        mesh: [
            Swatch(0xd3dcc4, 0x2c3529), // sage
            Swatch(0xeadbc0, 0x382f24), // sand
            Swatch(0xe8cfc2, 0x3a2925), // clay
            Swatch(0xcfdcd6, 0x24302d), // mist
            Swatch(0xefe2ca, 0x2c2820), // oat
        ],
        base: Swatch(0xf1ebe0, 0x1c1a17),
        ink: Swatch(0x2e2924, 0xebe4d6),
        muted: Swatch(0x5c5449, 0xb9af9f),
        line: Swatch(0x2e2924, 0xebe4d6, alpha: 0.12),
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
        sidebarTint: Swatch(0xfbf7ef, 0x191714, alpha: 0.62, darkAlpha: 0.45),
        glass: Swatch(0xfbf7ef, 0x191714, alpha: 0.74, darkAlpha: 0.74),
        bar: Swatch(0xfffdf8, 0xffffff, alpha: 0.35, darkAlpha: 0.03),
        cardTint: Swatch(0xfffdf8, 0xfff8ec, alpha: 0.82, darkAlpha: 0.06),
        edge: Swatch(0xffffff, 0xffffff, alpha: 0.7, darkAlpha: 0.1),
        meter: [Swatch(0x3d5f86, 0x9db8dc), Swatch(0xc9a25e, 0xd8b679), Swatch(0xc77c5c, 0xd99478)],
        prompt: [Swatch(0x3f6b5c, 0x3c6656), Swatch(0x3d5f86, 0x3a5878)],
        onPrompt: Swatch(0xfbf7ef),
        agentCard: Swatch(0xfffdf8, 0xfff8ec, alpha: 0.82, darkAlpha: 0.06),
        questionTint: Swatch(0xe6ebf2, 0x3d5f86, alpha: 0.95, darkAlpha: 0.26),
        approvalTint: Swatch(0xf7ebda, 0x8c4f0c, alpha: 1, darkAlpha: 0.26),
        command: Swatch(0x2e2924, 0x000000, alpha: 0.06, darkAlpha: 0.25),
        terminal: TerminalColours(
            background: Swatch(0x23211d), foreground: Swatch(0xe6dfd2),
            cursor: Swatch(0x9db8dc), selection: Swatch(0x9db8dc, alpha: 0.28),
            ansi: ansi(0x34302a, 0xe28e72, 0xa9c784, 0xe3c07a, 0x93b6cc, 0xcca3c3, 0x8fc5b5, 0xddd5c6,
                       0x857c6f, 0xeca68e, 0xbdd6a0, 0xeed39a, 0xabc8da, 0xdab8d2, 0xa8d7c9, 0xf4ede0)
        ),
        opaque: OpaqueColours(
            panel: Swatch(0xfbf8f2, 0x1a1815), bar: Swatch(0xf2ece1, 0x24211c), card: Swatch(0xfffdf8, 0x27241f),
            questionTint: Swatch(0xe3e9f1, 0x202c3c), approvalTint: Swatch(0xf6e9d7, 0x382916),
            edge: Swatch(0x5c5449, 0xc9bfae), muted: Swatch(0x433d34, 0xd6cdbd)
        ),
        titleFamily: "Bricolage Grotesque",
        monoFamily: "DM Mono"
    )

    /// Graphite, professional: neutral greys, one restrained blue, a
    /// near-flat backdrop and crisp hairline edges; system type.
    public static let graphite = ThemeColours(
        mesh: [
            Swatch(0xe3e6ea, 0x1c1f24),
            Swatch(0xdde2e9, 0x1a1e25),
            Swatch(0xe7e9ec, 0x1e2126),
            Swatch(0xdbe1e8, 0x181c22),
            Swatch(0xe6e9ee, 0x20242b),
        ],
        base: Swatch(0xeceef1, 0x17191d),
        ink: Swatch(0x16181d, 0xe8eaee),
        muted: Swatch(0x4a505a, 0xa3a9b3),
        line: Swatch(0x16181d, 0xe8eaee, alpha: 0.1),
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
        sidebarTint: Swatch(0xf7f8fa, 0x15171b, alpha: 0.72, darkAlpha: 0.6),
        glass: Swatch(0xffffff, 0x15171b, alpha: 0.8, darkAlpha: 0.78),
        bar: Swatch(0xffffff, 0xffffff, alpha: 0.45, darkAlpha: 0.03),
        cardTint: Swatch(0xffffff, 0xffffff, alpha: 0.92, darkAlpha: 0.05),
        edge: Swatch(0x000000, 0xffffff, alpha: 0.09, darkAlpha: 0.1),
        meter: [Swatch(0x2357c4, 0x76a5ff), Swatch(0x5b7fd6, 0x8fb4ff), Swatch(0x8aa6e6, 0xb3cbff)],
        prompt: [Swatch(0x2357c4, 0x2a5bc4), Swatch(0x1d4ba8, 0x234ea8)],
        onPrompt: Swatch(0xffffff),
        agentCard: Swatch(0xffffff, 0xffffff, alpha: 0.92, darkAlpha: 0.05),
        questionTint: Swatch(0xedf2fc, 0x2357c4, alpha: 1, darkAlpha: 0.22),
        approvalTint: Swatch(0xfcf1e8, 0xa84406, alpha: 1, darkAlpha: 0.22),
        command: Swatch(0x16181d, 0x000000, alpha: 0.05, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x111317), foreground: Swatch(0xd8dce2),
            cursor: Swatch(0x76a5ff), selection: Swatch(0x2357c4, alpha: 0.45),
            ansi: ansi(0x2a2e35, 0xff6b6b, 0x5fd38d, 0xe7c35a, 0x6ea0ff, 0xc792ea, 0x56c8d8, 0xd6dae0,
                       0x6b7280, 0xff8a8a, 0x7ee2a4, 0xf2d47a, 0x93b8ff, 0xd6aef2, 0x7ad6e3, 0xf5f7fa)
        ),
        opaque: OpaqueColours(
            panel: Swatch(0xffffff, 0x101215), bar: Swatch(0xf3f4f6, 0x1a1d22), card: Swatch(0xffffff, 0x1d2026),
            questionTint: Swatch(0xe8effc, 0x162540), approvalTint: Swatch(0xfdf0e4, 0x3a2412),
            edge: Swatch(0x3d434c, 0xb9bfc8), muted: Swatch(0x2f343c, 0xc9ced6)
        ),
        titleFamily: nil,
        monoFamily: nil
    )

    /// Dusk, dimmed for night work: very low light, muted violet and
    /// teal; its light is dimmed as well, lavender-grey with no pure
    /// white.
    public static let dusk = ThemeColours(
        mesh: [
            Swatch(0xcbc2de, 0x221a36), // violet
            Swatch(0xc2d4d5, 0x0f272a), // teal
            Swatch(0xd0c9da, 0x1a142d), // plum
            Swatch(0xc6d1d6, 0x0e2024), // sea
            Swatch(0xd4cddf, 0x191425), // haze
        ],
        base: Swatch(0xd8d4de, 0x0e0d13),
        ink: Swatch(0x221e2b, 0xd2cddf),
        muted: Swatch(0x48425a, 0x9a93ad),
        line: Swatch(0x221e2b, 0xd2cddf, alpha: 0.12),
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
        sidebarTint: Swatch(0xe6e2ec, 0x0b0a10, alpha: 0.6, darkAlpha: 0.55),
        glass: Swatch(0xe6e2ec, 0x0b0a10, alpha: 0.72, darkAlpha: 0.74),
        bar: Swatch(0xe9e6ef, 0xffffff, alpha: 0.35, darkAlpha: 0.02),
        cardTint: Swatch(0xebe8f0, 0xd2cddf, alpha: 0.82, darkAlpha: 0.05),
        edge: Swatch(0xf0eef4, 0xffffff, alpha: 0.6, darkAlpha: 0.07),
        meter: [Swatch(0x4f3f8c, 0xa596d6), Swatch(0x285e66, 0x8fc0c6), Swatch(0x8f3434, 0xdb8c8c)],
        prompt: [Swatch(0x4f3f8c, 0x3f3370), Swatch(0x2f5a66, 0x284c56)],
        onPrompt: Swatch(0xeceaf1, 0xddd8e8),
        agentCard: Swatch(0xebe8f0, 0xd2cddf, alpha: 0.82, darkAlpha: 0.05),
        questionTint: Swatch(0xdde4e8, 0x285e66, alpha: 0.95, darkAlpha: 0.24),
        approvalTint: Swatch(0xebe0d6, 0x7c4b14, alpha: 1, darkAlpha: 0.24),
        command: Swatch(0x221e2b, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x16141e, 0x111018), foreground: Swatch(0xc9c4d6, 0xbcb7c9),
            cursor: Swatch(0xa596d6), selection: Swatch(0xa596d6, alpha: 0.25),
            ansi: ansi(0x26232f, 0xd98a8a, 0x8cbfa6, 0xd6b47c, 0x93a6d8, 0xb39ad6, 0x84bdc3, 0xc9c4d6,
                       0x6e6884, 0xe6a3a3, 0xa3cfb9, 0xe0c496, 0xa9b9e2, 0xc4b0e0, 0x9ecbd0, 0xe0dcea)
        ),
        opaque: OpaqueColours(
            panel: Swatch(0xebe8f0, 0x0b0a10), bar: Swatch(0xe2dee9, 0x15131d), card: Swatch(0xefecf3, 0x1a1824),
            questionTint: Swatch(0xdde5ea, 0x14262a), approvalTint: Swatch(0xeee3d8, 0x2c2116),
            edge: Swatch(0x48425a, 0xa8a1ba), muted: Swatch(0x38334a, 0xbdb7cc)
        ),
        titleFamily: "Bricolage Grotesque",
        monoFamily: "DM Mono"
    )

    /// Aurora, vibrant: mint, cyan, violet and magenta lights on deep
    /// navy; an icy mesh by day.
    public static let aurora = ThemeColours(
        mesh: [
            Swatch(0x45f0c0, 0x0b8a6c), // mint
            Swatch(0x3ec8ff, 0x0a6fa0), // cyan
            Swatch(0xb47bff, 0x4a2aa8), // violet
            Swatch(0xff63d0, 0x8a1f80), // magenta
            Swatch(0x8fe8ff, 0x0c4a6a), // ice
        ],
        base: Swatch(0xe4f1ff, 0x071230),
        ink: Swatch(0x0b1a3a, 0xeef6ff),
        muted: Swatch(0x34446a, 0xb8c8e2),
        line: Swatch(0x0b1a3a, 0xeef6ff, alpha: 0.12),
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
        sidebarTint: Swatch(0xffffff, 0x061026, alpha: 0.62, darkAlpha: 0.45),
        glass: Swatch(0xffffff, 0x061026, alpha: 0.72, darkAlpha: 0.74),
        bar: Swatch(0xffffff, 0xffffff, alpha: 0.35, darkAlpha: 0.04),
        cardTint: Swatch(0xffffff, 0xd8ecff, alpha: 0.82, darkAlpha: 0.07),
        edge: Swatch(0xffffff, 0x8fe8ff, alpha: 0.85, darkAlpha: 0.16),
        meter: [Swatch(0x00a3a3, 0x5ee6d0), Swatch(0x3e8cff, 0x3ec8ff), Swatch(0xd23fd0, 0xff63d0)],
        prompt: [Swatch(0x00607e, 0x0a6386), Swatch(0x5a32c8, 0x5a32c0)],
        onPrompt: Swatch(0xffffff),
        agentCard: Swatch(0xffffff, 0xd8ecff, alpha: 0.82, darkAlpha: 0.07),
        questionTint: Swatch(0xecebff, 0x4636c8, alpha: 0.95, darkAlpha: 0.3),
        approvalTint: Swatch(0xfff0e3, 0xff8a3a, alpha: 1, darkAlpha: 0.2),
        command: Swatch(0x0b1a3a, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x08122b), foreground: Swatch(0xe1ecff),
            cursor: Swatch(0x5ee6d0), selection: Swatch(0x3ec8ff, alpha: 0.3),
            ansi: ansi(0x1a2546, 0xff6b8f, 0x5ef2a0, 0xffd76b, 0x6fa8ff, 0xe07bff, 0x4fe6ff, 0xdce8ff,
                       0x63759f, 0xff8faa, 0x8affbd, 0xffe598, 0x94bfff, 0xea9dff, 0x85eeff, 0xffffff)
        ),
        opaque: OpaqueColours(
            panel: Swatch(0xffffff, 0x061026), bar: Swatch(0xeef6ff, 0x0c1a3a), card: Swatch(0xffffff, 0x0f1f44),
            questionTint: Swatch(0xebe9ff, 0x1d1a55), approvalTint: Swatch(0xfff0e3, 0x3a2414),
            edge: Swatch(0x34446a, 0xb8c8e2), muted: Swatch(0x2b3858, 0xcfdcf0)
        ),
        titleFamily: "Bricolage Grotesque",
        monoFamily: "DM Mono"
    )

    /// Sunset, vibrant and warm: coral, amber and pink; raspberry
    /// actions, glowing embers in dark.
    public static let sunset = ThemeColours(
        mesh: [
            Swatch(0xff7a59, 0x8a2b26), // coral
            Swatch(0xffb43b, 0x8a5410), // amber
            Swatch(0xff6fa3, 0x7a1f4f), // pink
            Swatch(0xffc59a, 0x5a2a1a), // peach
            Swatch(0xff8fb5, 0x4a1430), // rose
        ],
        base: Swatch(0xffe6d6, 0x1e0f14),
        ink: Swatch(0x2b1418, 0xfff1ea),
        muted: Swatch(0x663b42, 0xe2c3ba),
        line: Swatch(0x2b1418, 0xfff1ea, alpha: 0.12),
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
        sidebarTint: Swatch(0xfffaf6, 0x1a0b10, alpha: 0.62, darkAlpha: 0.45),
        glass: Swatch(0xfffaf6, 0x1a0b10, alpha: 0.72, darkAlpha: 0.74),
        bar: Swatch(0xffffff, 0xffffff, alpha: 0.35, darkAlpha: 0.04),
        cardTint: Swatch(0xfffcfa, 0xffe6d6, alpha: 0.82, darkAlpha: 0.07),
        edge: Swatch(0xffffff, 0xffc59a, alpha: 0.85, darkAlpha: 0.16),
        meter: [Swatch(0x951d66, 0xe78ae0), Swatch(0xff6fa3), Swatch(0xffb43b)],
        prompt: [Swatch(0xa8234f, 0xa8234f), Swatch(0xb8400c, 0xb03c0c)],
        onPrompt: Swatch(0xffffff),
        agentCard: Swatch(0xfffcfa, 0xffe6d6, alpha: 0.82, darkAlpha: 0.07),
        questionTint: Swatch(0xf4e9ff, 0x6f2aa6, alpha: 0.95, darkAlpha: 0.3),
        approvalTint: Swatch(0xfff0e2, 0xff8a3a, alpha: 1, darkAlpha: 0.2),
        command: Swatch(0x2b1418, 0x000000, alpha: 0.06, darkAlpha: 0.3),
        terminal: TerminalColours(
            background: Swatch(0x1f1216), foreground: Swatch(0xfbe9e1),
            cursor: Swatch(0xe78ae0), selection: Swatch(0xff6fa3, alpha: 0.3),
            ansi: ansi(0x33202a, 0xff7a6b, 0xa6e07a, 0xffc861, 0x8fb0ff, 0xff8fc8, 0x6fd8d0, 0xf3e2dc,
                       0x86666e, 0xff9a8c, 0xbdeb98, 0xffd88a, 0xadc4ff, 0xffaad6, 0x94e4de, 0xfff6f2)
        ),
        opaque: OpaqueColours(
            panel: Swatch(0xfffaf7, 0x180b10), bar: Swatch(0xfff0e8, 0x26131a), card: Swatch(0xffffff, 0x2b161e),
            questionTint: Swatch(0xf3e8ff, 0x2f1a40), approvalTint: Swatch(0xfff0e6, 0x3d2010),
            edge: Swatch(0x663b42, 0xe2c3ba), muted: Swatch(0x4f2c31, 0xf0d6cd)
        ),
        titleFamily: "Bricolage Grotesque",
        monoFamily: "DM Mono"
    )

    /// Solar, high legibility: near-black ink on cream (white on black
    /// in dark), firm edges, nearly opaque panels, a sun-gold mesh;
    /// system type.
    public static let solar = ThemeColours(
        mesh: [
            Swatch(0xfbe3a0, 0x2a2000), // sun
            Swatch(0xfdf0cc, 0x141414),
            Swatch(0xf6dfae, 0x0f1a2a),
            Swatch(0xfff2d0, 0x1f1010),
            Swatch(0xfbe39a, 0x262000), // gold
        ],
        base: Swatch(0xfdf6e3, 0x0a0a0a),
        ink: Swatch(0x0b0b0b, 0xffffff),
        muted: Swatch(0x38332a, 0xd8d2c2),
        line: Swatch(0x0b0b0b, 0xffffff, alpha: 0.2),
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
        sidebarTint: Swatch(0xfffaf0, 0x000000, alpha: 0.86, darkAlpha: 0.72),
        glass: Swatch(0xfffdf6, 0x050505, alpha: 0.9, darkAlpha: 0.86),
        bar: Swatch(0xffffff, 0xffffff, alpha: 0.5, darkAlpha: 0.05),
        cardTint: Swatch(0xffffff, 0xffffff, alpha: 0.95, darkAlpha: 0.08),
        edge: Swatch(0x000000, 0xffffff, alpha: 0.2, darkAlpha: 0.3),
        meter: [Swatch(0x0045ad, 0xffd23f), Swatch(0x3a6fd0, 0xffe17a), Swatch(0x7a9fe6, 0xfff0b0)],
        prompt: [Swatch(0x0045ad, 0x1a4fb0), Swatch(0x003a94, 0x123f94)],
        onPrompt: Swatch(0xffffff),
        agentCard: Swatch(0xffffff, 0xffffff, alpha: 0.95, darkAlpha: 0.08),
        questionTint: Swatch(0xf0eaff, 0x5428ad, alpha: 1, darkAlpha: 0.3),
        approvalTint: Swatch(0xfff1dc, 0xffaa4d, alpha: 1, darkAlpha: 0.16),
        command: Swatch(0x0b0b0b, 0x000000, alpha: 0.07, darkAlpha: 0.5),
        terminal: TerminalColours(
            background: Swatch(0x0a0a0a), foreground: Swatch(0xf5f5f5),
            cursor: Swatch(0xffd23f), selection: Swatch(0xffd23f, alpha: 0.3),
            ansi: ansi(0x262626, 0xff5f5f, 0x5fe08a, 0xffd23f, 0x6aa8ff, 0xff7ae0, 0x3fe0e0, 0xe8e8e8,
                       0x8a8a8a, 0xff8a8a, 0x8af0a8, 0xffe17a, 0x9cc4ff, 0xff9eea, 0x7aeeee, 0xffffff)
        ),
        opaque: OpaqueColours(
            panel: Swatch(0xffffff, 0x000000), bar: Swatch(0xfdf6e3, 0x141414), card: Swatch(0xffffff, 0x161616),
            questionTint: Swatch(0xefe8ff, 0x1f1a33), approvalTint: Swatch(0xfff0dc, 0x2e1d08),
            edge: Swatch(0x000000, 0xffffff), muted: Swatch(0x1f1c14, 0xe8e3d6)
        ),
        titleFamily: nil,
        monoFamily: nil
    )

    private static func ansi(_ c: UInt32...) -> [Swatch] {
        c.map { Swatch($0) }
    }
}

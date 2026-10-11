package mod

// EnvNoMods is safe mode (mods design §9): set by tm --no-mods, which
// passes it on to a server it starts, or by hand for the server
// (TERMINATR_NO_MODS=1 tm server run). No user mod runs then, so a mod
// can never lock someone out of their own dashboard; bundled mods still
// do. The mods stay installed and enabled, and can be fixed, disabled or
// removed.
const EnvNoMods = "TERMINATR_NO_MODS"

// NoMods reports whether getenv asks for safe mode: EnvNoMods set to
// anything but "" or "0".
func NoMods(getenv func(string) string) bool {
	v := getenv(EnvNoMods)
	return v != "" && v != "0"
}

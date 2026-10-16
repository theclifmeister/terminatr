package agent

import "github.com/theclifmeister/terminatr/internal/proto"

// Infos describes every agent in reg, as tm agent list and the server's
// agent.list show them.
func Infos(reg *Registry) []proto.AgentInfo {
	out := []proto.AgentInfo{}
	for _, n := range reg.Names() {
		a, _ := reg.Get(n)
		info := proto.AgentInfo{Name: n, Source: reg.Source[n], Injector: string(a.Injector())}
		if m := ManifestOf(a); m != nil {
			info.Display, info.Command, info.Tested = m.Display, m.Launch.Command, m.TestedVersions
			info.LastTested = m.Identify.LastTested
			info.Unenforced = !m.RendersAccess()
		}
		out = append(out, info)
	}
	return out
}

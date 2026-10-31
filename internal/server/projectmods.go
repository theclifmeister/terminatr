package server

// mods.list, mods.set and mods.setting (proto.CapProjectMods, docs/SPEC.md
// §3.8): a project's mods for the Mac app and paired devices, as the
// project popup's Mods tab shows and changes them (internal/modproj has
// its words and rules). The human's only: the human socket's, and a
// device's through its app API (reads at view level, changes at write
// level, appapi.Allowed); the mod functions refuse an agent besides.
// Turning a mod on grants what it asks for, so it is done only for the
// key of the consent the console showed (modproj.Key). Every change is
// journaled in the project as its caller, and a device's is logged.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modproj"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// projectMods are the installed mods and this server's projects, for
// project p's slug, which must be one of them.
func (s *Server) projectMods(slug string) (*mod.Registry, []string, *proto.Error) {
	if _, err := project.Open(slug); err != nil {
		return nil, nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	reg, _, _ := s.installedMods(0)
	if reg == nil {
		return nil, nil, proto.Errorf(proto.ErrInternal, "the mods can't be listed: tm server log says why")
	}
	list, err := project.List()
	if err != nil {
		return nil, nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	var locals []string
	for _, p := range list {
		locals = append(locals, p.Slug)
	}
	return reg, locals, nil
}

func (s *Server) modsList(p proto.ModsListParams) (any, *proto.Error) {
	reg, locals, perr := s.projectMods(p.Project)
	if perr != nil {
		return nil, perr
	}
	out := proto.ModsListResult{Project: p.Project, Mods: []proto.ProjectMod{}}
	for _, md := range reg.Mods {
		out.Mods = append(out.Mods, projectModOf(md, p.Project, locals, md.ID == p.Consent))
	}
	return out, nil
}

// projectModOf is mod md for project slug; consent, with its consent's
// lines when turning it on asks for it.
func projectModOf(md mod.Mod, slug string, locals []string, consent bool) proto.ProjectMod {
	where, tone := modproj.Where(md, slug)
	out := proto.ProjectMod{ID: md.ID, Name: modproj.Name(md), Kinds: modproj.Kinds(md), Runs: md.Runs(slug),
		Where: where, WhereTone: tone, Own: modproj.OwnSettings(md, slug), Required: md.Required, Builtin: md.Builtin}
	if md.Manifest != nil {
		out.Version = md.Manifest.Version
		out.Description = strings.TrimSpace(mod.Clean(md.Manifest.Description, false))
	}
	t := modproj.ToggleFor(md, slug, locals)
	out.Toggle = proto.ModToggle{Do: t.Do, Title: t.Title, Question: t.Question, Why: t.Why, Done: t.Done,
		Others: t.Others, Key: modproj.KeyOf(md, t)}
	if lines, _ := modproj.Asked(md, t); consent && lines != nil {
		for _, l := range lines {
			cl := proto.ConsentLine{Fit: l.Fit, Spans: []proto.ConsentSpan{}}
			for _, sp := range l.Spans {
				cl.Spans = append(cl.Spans, proto.ConsentSpan{Text: sp.Text, Tone: sp.Tone})
			}
			out.Toggle.Consent = append(out.Toggle.Consent, cl)
		}
	}
	switch {
	case md.Manifest == nil || len(md.Manifest.Settings) == 0:
		out.SettingsNote = "It has no settings."
	case md.Builtin:
		out.SettingsNote = "Built in: its settings are tm's look and layout, the same for every project here."
	default:
		cur := md.SettingsFor(slug)
		keys := make([]string, 0, len(md.Manifest.Settings))
		for k := range md.Manifest.Settings {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			def := md.Manifest.Settings[k]
			_, own := md.ProjectSettings[slug][k]
			ms := proto.ModSetting{Key: k, Label: mod.Clean(def.Label, false), Type: def.Type,
				Default: modValueOf(def.Default), Value: modValueOf(cur[k]), Own: own, Min: def.Min, Max: def.Max,
				Options: slices.Clone(def.Options)}
			if def.Type == mod.SettingString {
				ms.MaxLen = mod.MaxTextSetting
			}
			out.Settings = append(out.Settings, ms)
		}
	}
	prefix := "projects." + slug + ": "
	for _, b := range md.BadSettings {
		if strings.HasPrefix(b, prefix) {
			out.Bad = append(out.Bad, "Setting "+b)
		}
	}
	return out
}

// modValueOf is a setting's value as a ModValue: an int, a bool or a
// text; none for another.
func modValueOf(v any) proto.ModValue {
	switch x := v.(type) {
	case bool:
		return proto.ModValue{Bool: &x}
	case int64:
		return proto.ModValue{Int: &x}
	case int:
		n := int64(x)
		return proto.ModValue{Int: &n}
	case string:
		return proto.ModValue{Text: &x}
	}
	return proto.ModValue{}
}

// modsSet turns p.Mod on or off for p.Project as the Mods tab's space
// does, when that is what the console showed: p.On says which way, and
// p.Key is the key of what it asked first (the consent, or the question
// limiting a mod that runs for every project).
func (s *Server) modsSet(p proto.ModsSetParams, c callCtx) (any, *proto.Error) {
	reg, locals, perr := s.projectMods(p.Project)
	if perr != nil {
		return nil, perr
	}
	md, ok := reg.Get(p.Mod)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "no mod %q is installed", p.Mod)
	}
	t := modproj.ToggleFor(md, p.Project, locals)
	if t.Do == modproj.DoRefuse {
		return nil, proto.Errorf(proto.ErrRefused, "%s", t.Why)
	}
	if on := t.Do == modproj.DoOn || t.Do == modproj.DoConsent; on != p.On {
		state := map[bool]string{true: "off", false: "on"}[on]
		return nil, proto.Errorf(proto.ErrModChanged, "%s is %s for %s already: nothing changed", modproj.Name(md), state, p.Project)
	}
	if key := modproj.KeyOf(md, t); key != p.Key {
		return nil, proto.Errorf(proto.ErrModChanged, "what turning %s %s for %s asks changed since it was shown: nothing changed, look again",
			modproj.Name(md), map[bool]string{true: "on", false: "off"}[p.On], p.Project)
	}
	who := c.who()
	var err error
	action, detail := "mod.disable", "off for "+p.Project
	switch t.Do {
	case modproj.DoOff:
		err = mod.Disable(who, md.ID, p.Project)
	case modproj.DoOffAll:
		err, detail = mod.Disable(who, md.ID, ""), "off (it ran for every project, "+p.Project+" the only one)"
	case modproj.DoLimit:
		action, detail = "mod.projects", "off for "+p.Project+": runs only for "+strings.Join(t.Others, ", ")
		err = mod.SetProjects(who, md.ID, t.Others)
	default:
		action, detail = "mod.enable", "on for "+p.Project
		if t.Do == modproj.DoConsent {
			detail += " after its consent (" + t.Question + "; key " + p.Key + ")"
		}
		err = mod.Enable(who, md.ID, p.Project)
	}
	if err != nil {
		return nil, codeErr(err)
	}
	s.modChanged(p.Project, who, c, action, md.ID, detail)
	return proto.ModsActResult{Words: t.Done}, nil
}

// modsSetting sets p.Key of mod p.Mod for p.Project, or with no value has
// it follow all projects again.
func (s *Server) modsSetting(p proto.ModsSettingParams, c callCtx) (any, *proto.Error) {
	reg, _, perr := s.projectMods(p.Project)
	if perr != nil {
		return nil, perr
	}
	md, ok := reg.Get(p.Mod)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "no mod %q is installed", p.Mod)
	}
	name := modproj.Name(md)
	switch {
	case md.Manifest == nil || len(md.Manifest.Settings) == 0:
		return nil, proto.Errorf(proto.ErrRefused, "%s has no settings", name)
	case md.Builtin:
		return nil, proto.Errorf(proto.ErrRefused, "%s is built in: its settings are the same for every project here", name)
	}
	def, ok := md.Manifest.Settings[p.Key]
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%s has no setting %q", name, p.Key)
	}
	var value any
	words := p.Key + ": follows all projects"
	if p.Value != nil {
		v := *p.Value
		n := 0
		for _, set := range []bool{v.Bool != nil, v.Int != nil, v.Text != nil} {
			if set {
				n++
			}
		}
		if n != 1 {
			return nil, proto.Errorf(proto.ErrBadParams, "a value is one of bool, int and text")
		}
		switch {
		case v.Bool != nil:
			value = *v.Bool
		case v.Int != nil:
			value = *v.Int
		default:
			value = *v.Text
		}
		checked, err := mod.CheckSetting(md.Manifest, p.Key, value)
		if err != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
		}
		words = settingWords(p.Key, def, checked)
	}
	who := c.who()
	if err := mod.SetSettingFor(who, md.ID, p.Project, p.Key, value); err != nil {
		return nil, codeErr(err)
	}
	detail := words
	if def.Type == mod.SettingString && value != nil {
		detail = p.Key + " set"
	}
	s.modChanged(p.Project, who, c, "mod.setting", md.ID, detail)
	return proto.ModsActResult{Words: words}, nil
}

// settingWords are what the TUI says once setting key is v.
func settingWords(key string, def mod.Setting, v any) string {
	switch def.Type {
	case mod.SettingBool:
		if b, _ := v.(bool); b {
			return key + " on"
		}
		return key + " off"
	case mod.SettingString:
		return key + " saved"
	case mod.SettingInt:
		return fmt.Sprintf("%s: %v", key, v)
	}
	return key + ": " + mod.Clean(fmt.Sprint(v), false)
}

// modChanged has the mods read anew, the consoles told, and the change
// journaled in project slug as who; a device's is logged too.
func (s *Server) modChanged(slug string, who caller.Caller, c callCtx, action, id, detail string) {
	s.forgetMods()
	s.kick()
	s.journalIn(slug, who, action, id, detail)
	if c.device != "" {
		s.log.Printf("device %s: %s %s for project %s: %s", c.device, action, id, slug, detail)
	}
}

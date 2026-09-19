package hotkey

import "testing"

func TestParseAndFormat(t *testing.T) {
	cases := map[string]struct {
		str  string
		vk   uint32
		mods uint32
	}{
		"Ctrl+Shift+F10":  {"Ctrl+Shift+F10", 0x79, ModCtrl | ModShift},
		"shift + ctrl+f9": {"Ctrl+Shift+F9", 0x78, ModCtrl | ModShift},
		"alt+d":           {"Alt+D", 'D', ModAlt},
		"Ctrl+Alt+home":   {"Ctrl+Alt+Home", 0x24, ModCtrl | ModAlt},
		"Win+Shift+1":     {"Shift+Win+1", '1', ModShift | ModWin},
		"ctrl+f24":        {"Ctrl+F24", 0x87, ModCtrl},
	}
	for in, want := range cases {
		h, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if h.String() != want.str || h.VK() != want.vk || h.Mods() != want.mods {
			t.Errorf("Parse(%q) = %s vk %#x mods %#x", in, h, h.VK(), h.Mods())
		}
	}
	for _, bad := range []string{"", "F10", "Shift+A", "Ctrl+", "Ctrl+F25", "Ctrl+F1x", "Ctrl+Banana", "Ctrl+A+B"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

func TestX11Keysyms(t *testing.T) {
	for combo, want := range map[string]uint32{"Ctrl+Shift+F10": 0xffc7, "Ctrl+Shift+F9": 0xffc6, "Alt+D": 'd', "Ctrl+1": '1', "Ctrl+Home": 0xff50, "Ctrl+Num5": 0xffb5} {
		h, err := Parse(combo)
		if err != nil || h.Keysym() != want {
			t.Errorf("%s: keysym %#x, want %#x (%v)", combo, h.Keysym(), want, err)
		}
	}
}

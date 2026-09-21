// Package hotkey parses and formats global shortcuts such as "Ctrl+Shift+F10".
package hotkey

import (
	"errors"
	"fmt"
	"strings"
)

type Hotkey struct {
	Ctrl, Alt, Shift, Win bool
	Key                   string // "F10", "A", "1", "Home"
}

// Windows modifier flags for RegisterHotKey.
const (
	ModAlt   = 0x1
	ModCtrl  = 0x2
	ModShift = 0x4
	ModWin   = 0x8
)

var namedKeys = map[string]uint32{
	"Space": 0x20, "PageUp": 0x21, "PageDown": 0x22, "End": 0x23, "Home": 0x24,
	"Insert": 0x2D, "Delete": 0x2E, "Pause": 0x13,
	"Num0": 0x60, "Num1": 0x61, "Num2": 0x62, "Num3": 0x63, "Num4": 0x64,
	"Num5": 0x65, "Num6": 0x66, "Num7": 0x67, "Num8": 0x68, "Num9": 0x69,
}

// VK returns the Windows virtual-key code for the key.
func (h Hotkey) VK() uint32 {
	k := h.Key
	switch {
	case len(k) == 1 && (k[0] >= 'A' && k[0] <= 'Z' || k[0] >= '0' && k[0] <= '9'):
		return uint32(k[0])
	case len(k) >= 2 && k[0] == 'F':
		var n int
		if _, err := fmt.Sscanf(k[1:], "%d", &n); err == nil && n >= 1 && n <= 24 && fmt.Sprint(n) == k[1:] {
			return 0x70 + uint32(n-1)
		}
	}
	return namedKeys[k]
}

// Keysym is the key's X11 keysym, for grabbing it on Linux.
func (h Hotkey) Keysym() uint32 {
	k := h.Key
	switch vk := h.VK(); {
	case len(k) == 1 && k[0] >= 'A' && k[0] <= 'Z':
		return uint32(k[0]-'A') + 'a'
	case len(k) == 1 && k[0] >= '0' && k[0] <= '9':
		return uint32(k[0])
	case vk >= 0x70 && vk <= 0x87:
		return 0xffbe + vk - 0x70
	case vk >= 0x60 && vk <= 0x69:
		return 0xffb0 + vk - 0x60
	}
	return x11Keys[k]
}

var x11Keys = map[string]uint32{
	"Space": 0x20, "PageUp": 0xff55, "PageDown": 0xff56, "End": 0xff57, "Home": 0xff50,
	"Insert": 0xff63, "Delete": 0xffff, "Pause": 0xff13,
}

// Mods returns the RegisterHotKey modifier flags.
func (h Hotkey) Mods() uint32 {
	var m uint32
	if h.Ctrl {
		m |= ModCtrl
	}
	if h.Alt {
		m |= ModAlt
	}
	if h.Shift {
		m |= ModShift
	}
	if h.Win {
		m |= ModWin
	}
	return m
}

func (h Hotkey) String() string {
	var parts []string
	for _, p := range []struct {
		on   bool
		name string
	}{{h.Ctrl, "Ctrl"}, {h.Alt, "Alt"}, {h.Shift, "Shift"}, {h.Win, "Win"}} {
		if p.on {
			parts = append(parts, p.name)
		}
	}
	return strings.Join(append(parts, h.Key), "+")
}

// Parse reads a shortcut like "ctrl+shift+f10". It needs Ctrl, Alt or Win, so a shortcut
// can't swallow ordinary typing or game keys.
func Parse(s string) (Hotkey, error) {
	var h Hotkey
	parts := strings.Split(strings.ReplaceAll(s, " ", ""), "+")
	for i, p := range parts {
		last := i == len(parts)-1
		switch strings.ToLower(p) {
		case "ctrl", "control":
			h.Ctrl = true
		case "alt":
			h.Alt = true
		case "shift":
			h.Shift = true
		case "win", "meta":
			h.Win = true
		default:
			if !last || p == "" {
				return Hotkey{}, fmt.Errorf("%q isn't a shortcut like Ctrl+Shift+F10", s)
			}
			h.Key = canonicalKey(p)
		}
	}
	switch {
	case h.Key == "" || h.VK() == 0:
		return Hotkey{}, fmt.Errorf("%q needs a key such as F10, a letter or a digit", s)
	case !h.Ctrl && !h.Alt && !h.Win:
		return Hotkey{}, errors.New("a shortcut needs Ctrl, Alt or Win so it doesn't block game keys")
	}
	return h, nil
}

func canonicalKey(k string) string {
	if len(k) == 1 {
		return strings.ToUpper(k)
	}
	for name := range namedKeys {
		if strings.EqualFold(name, k) {
			return name
		}
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

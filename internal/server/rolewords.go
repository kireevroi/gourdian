package server

import "gourdian/internal/i18n"

// roleWordsRU words the position messages in Russian, keyed by the English.

func roleSay(lang, format string, args ...any) string { return i18n.Say(lang, format, args...) }

func laneIn(lang, lane string) string { return i18n.Word(lang, "in the "+lane) }

package server

import "gourdian/internal/i18n"

func laneIn(lang, lane string) string { return i18n.Word(lang, "in the "+lane) }

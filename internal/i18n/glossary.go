package i18n

// Term is a game term as the trainer writes it in Russian, with other wordings it must not
// use, so the rules, HUD, voice and dashboard all say the same thing.
type Term struct {
	EN  string
	RU  string   // the wording the trainer uses
	Not []string // other wordings, as the start of a word, in lower case
}

// Glossary follows the Russian Dota client where it names the thing.
var Glossary = []Term{
	{EN: "Shrine of Wisdom", RU: "Святилище мудрости", Not: []string{"святын"}},
	{EN: "Aegis", RU: "Аегис", Not: []string{"эгид", "аегид"}},
	{EN: "Tormentor", RU: "Торментор", Not: []string{"мучител"}},
	{EN: "Power rune", RU: "Руна силы", Not: []string{"руна мощи", "руны мощи"}},
	{EN: "Bounty rune", RU: "Руна богатства", Not: []string{"баунти"}},
	{EN: "Buyback", RU: "Выкуп", Not: []string{"байбэк", "бай-бэк", "байбек"}},
	{EN: "Last hits", RU: "Добивания", Not: []string{"ластхит", "ласт-хит"}},
	{EN: "Carry", RU: "Керри", Not: []string{"кэрри"}},
	{EN: "TP scroll", RU: "Свиток телепорта", Not: []string{"тп-свит", "свиток тп"}},
}

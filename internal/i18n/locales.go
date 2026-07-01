// Copyright (c) 2026 Aristarh Ucolov.
//
// Additional UI locales. Each language lives in its own locale_<code>.go file
// and carries a FULL translation of every key in the English base bundle, so
// nothing falls back to English. Get() still overlays on English defensively,
// so if a key is ever added to en and missed here the UI degrades gracefully
// instead of showing a raw key. The i18n parity test enforces completeness.
package i18n

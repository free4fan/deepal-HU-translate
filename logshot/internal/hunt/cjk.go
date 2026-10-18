// CJK-охота: собирает ВСЕ китайские фрагменты из снимка logcat
// (неизвестные заранее — их ещё нет в прошивке/APK, они приходят
// runtime). Агрегирует по токенам (идеограммы: CJK Unified + Ext A/B,
// Compat); CJK-пунктуация и латиница — разделители. Атрибуция
// pid→приложение — та же, что и у охоти за фразой.
package hunt

import (
	"regexp"
	"sort"
	"strconv"
)

// reCJK — непрерывные последовательности CJK-идеограмм (без пунктуации):
// 3400-4DBF Ext A, 4E00-9FFF Unified, F900-FAFF Compat,
// 20000-2A6DF Ext B, 2A700-2B73F Ext C, 2B740-2B81F Ext D,
// 2B820-2BEAF Ext E. ДИАПАЗОНЫ НЕ ПЕРЕСЕКАЮТСЯ (иначе RE2 «invalid
// character class range»). 30000+ (Ext F/G) в UI не встречается —
// не добавляем, чтобы не рисковать валидностью класса.
var reCJK = regexp.MustCompile(
	`[\x{3400}-\x{4DBF}\x{4E00}-\x{9FFF}\x{F900}-\x{FAFF}` +
		`\x{20000}-\x{2A6DF}\x{2A700}-\x{2B73F}\x{2B740}-\x{2B81F}` +
		`\x{2B820}-\x{2BEAF}]+`)

// CJKToken — уникальная CJK-фрагмента (токен).
type CJKToken struct {
	Text  string
	Count int         // сколько раз встречено в снимке
	First int         // индекс строки снимка (первое вхождение)
	Pid   int         // pid, на которого пришлось max встречений (см. pidC)
	Tags  string      // tag при dominant-вхождении
	Ex    []string    // образцы строк (до 2)
	pidC  map[int]int // pid -> встречений именно этого токена
}

// CJKReport — итог CJK-охоты.
type CJKReport struct {
	TotalLines  int
	CJKLines    int            // строк снимка, где есть хоть одна CJK-фрагмента
	Tokens      []CJKToken     // отсортированно: по частоте desc, затем Text
	EmitPids    map[int]int    // pid -> общее число CJK-встречений (весь снимок)
	Attribution map[int]string // pid -> имя/пакет (заполняется AttrPids)
}

// CollectCJK — сканирует строки снимка, агрегирует CJK-токены.
func CollectCJK(lines []string) CJKReport {
	rep := CJKReport{
		TotalLines:  len(lines),
		EmitPids:    map[int]int{},
		Attribution: map[int]string{},
	}
	byText := map[string]int{} // text -> индекс в tokens
	for i, raw := range lines {
		toks := reCJK.FindAllString(raw, -1)
		if len(toks) == 0 {
			continue
		}
		rep.CJKLines++
		l := ParseLine(raw) // pid/tag для атрибуции (если строка распознана)
		pid, tag := 0, ""
		if l.OK {
			pid, tag = l.PID, l.Tag
		}
		for _, t := range toks {
			idx, ok := byText[t]
			if !ok {
				tok := CJKToken{Text: t, First: i, pidC: map[int]int{}}
				byText[t] = len(rep.Tokens)
				rep.Tokens = append(rep.Tokens, tok)
				idx = len(rep.Tokens) - 1
			}
			to := &rep.Tokens[idx]
			to.Count++
			if to.Count <= 2 {
				to.Ex = append(to.Ex, raw)
			}
			if pid > 0 {
				rep.EmitPids[pid]++
				to.pidC[pid]++
				if to.pidC[pid] > to.pidC[to.Pid] {
					to.Pid, to.Tags = pid, tag
				}
			}
		}
	}
	sortTokens(&rep)
	return rep
}

func sortTokens(r *CJKReport) {
	// по Count desc, затем по Text asc (стабильно)
	sort.SliceStable(r.Tokens, func(i, j int) bool {
		if r.Tokens[i].Count != r.Tokens[j].Count {
			return r.Tokens[i].Count > r.Tokens[j].Count
		}
		return r.Tokens[i].Text < r.Tokens[j].Text
	})
}

// AttrPids — подмешивает pid→имя (совместимо с Report.AttrPids, но
// для CJKReport).
func (r *CJKReport) AttrPids(attr map[int]string) {
	for k, v := range attr {
		r.Attribution[k] = v
	}
}

// UniqueEmitPids — pids, печатавшие CJK (сортир. desc по встречаниям).
func (r *CJKReport) UniqueEmitPids() []int {
	type kp struct{ p, n int }
	var out []kp
	for p, n := range r.EmitPids {
		out = append(out, kp{p, n})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].p < out[j].p
	})
	res := make([]int, len(out))
	for i, o := range out {
		res[i] = o.p
	}
	return res
}

// Format — человек-читаемый отчёт.
func (r *CJKReport) Format(label string) string {
	var b []byte
	b = append(b, "==== CJK fragments report (logshot) ====\n"...)
	b = append(b, "label: "+label+"\n"...)
	b = append(b, "lines in shot: "+itoa(r.TotalLines)+"\n"...)
	b = append(b, "lines with CJK: "+itoa(r.CJKLines)+"\n"...)
	b = append(b, "unique tokens: "+itoa(len(r.Tokens))+"\n\n"...)

	b = append(b, "## top (по частоте) ##\n"...)
	for _, t := range r.Tokens {
		b = append(b, line("  "+itoa(t.Count)+"x  "+t.Text)...)
		if t.Pid > 0 {
			b = append(b, line("       pid="+itoa(t.Pid))...)
			if name := r.Attribution[t.Pid]; name != "" {
				b = append(b, []byte(name)...)
				b = append(b, []byte("\n")...)
			}
			b = append(b, line("       tag="+t.Tags)...)
		}
		for _, ex := range t.Ex {
			b = append(b, line("  ex: "+ex)...)
		}
		b = append(b, '\n')
	}

	b = append(b, "## pids emitting CJK (desc) ##\n"...)
	for _, p := range r.UniqueEmitPids() {
		line := "  pid=" + itoa(p) + " encounters=" + itoa(r.EmitPids[p])
		if name := r.Attribution[p]; name != "" {
			line += "  " + name
		}
		b = append(b, []byte(line)...)
		b = append(b, '\n')
	}
	b = append(b, "==== end ====\n"...)
	return string(b)
}

func line(s string) []byte { return []byte(s + "\n") }
func itoa(n int) string    { return strconv.Itoa(n) }

package msg

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// verbs 按参数位置列出格式串里的动词，如 "%s：%[2]d" → [s d]；%% 不算。
var verbRe = regexp.MustCompile(`%(?:\[(\d+)\])?[-+# 0]*\d*(?:\.\d+)?([a-zA-Z%])`)

func verbs(s string) []string {
	var out []string
	next := 0
	for _, m := range verbRe.FindAllStringSubmatch(s, -1) {
		if m[2] == "%" {
			continue
		}
		i := next
		if m[1] != "" {
			i = int(m[1][0]-'0') - 1
		}
		for len(out) <= i {
			out = append(out, "")
		}
		out[i] = m[2]
		next = i + 1
	}
	return out
}

// eachString 遍历 Text 里的每个字符串（含 Count 和嵌套结构里的），给出字段路径。
func eachString(v reflect.Value, path string, f func(path, s string)) {
	switch v.Kind() {
	case reflect.String:
		f(path, v.String())
	case reflect.Struct:
		for i := range v.NumField() {
			eachString(v.Field(i), path+"."+v.Type().Field(i).Name, f)
		}
	}
}

func strs(t *Text) map[string]string {
	m := map[string]string{}
	eachString(reflect.ValueOf(*t), "", func(p, s string) { m[p] = s })
	return m
}

func TestCatalogsMatch(t *testing.T) {
	z, e := strs(For(ZH)), strs(For(EN))
	if len(z) != len(e) || len(z) < 100 {
		t.Fatalf("两套文案项数应相同：中文 %d、英文 %d", len(z), len(e))
	}
	for p, zs := range z {
		es := e[p]
		if zs == "" || es == "" {
			t.Errorf("%s 有一种语言是空的", p)
		}
		if p == ".Usage" {
			continue // 原样输出，不当格式串
		}
		if zv, ev := verbs(zs), verbs(es); !reflect.DeepEqual(zv, ev) {
			t.Errorf("%s 两种语言的参数对不上：中文 %v %q，英文 %v %q", p, zv, zs, ev, es)
		}
	}
}

func TestEnglishHasNoChinese(t *testing.T) {
	for p, s := range strs(For(EN)) {
		for _, r := range s {
			if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF) {
				t.Errorf("英文文案 %s 里有中文字符或全角标点 %q：%q", p, r, s)
				break
			}
		}
	}
}

func TestCountPlural(t *testing.T) {
	if got := For(EN).Repos.N(1); got != "1 repo" {
		t.Fatalf("got %q", got)
	}
	if got := For(EN).Repos.N(3); got != "3 repos" {
		t.Fatalf("got %q", got)
	}
	if got := For(ZH).Repos.N(1); got != "1 个仓库" {
		t.Fatalf("got %q", got)
	}
}

func TestFromEnv(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want Lang
	}{
		{map[string]string{}, EN},
		{map[string]string{"LANG": "zh_CN.UTF-8"}, ZH},
		{map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{map[string]string{"LANG": "C"}, EN},
		{map[string]string{"LANG": "zh_CN.UTF-8", "LC_MESSAGES": "en_US.UTF-8"}, EN},
		{map[string]string{"LANG": "en_US.UTF-8", "LC_MESSAGES": "zh_TW.UTF-8"}, ZH},
		{map[string]string{"LC_MESSAGES": "zh_CN.UTF-8", "LC_ALL": "en_GB.UTF-8"}, EN},
		{map[string]string{"LC_ALL": "zh_HK", "LANG": "en_US"}, ZH},
		{map[string]string{"LC_ALL": "", "LANG": "zh_CN"}, ZH}, // 空值跳过
	}
	for _, c := range cases {
		if got := FromEnv(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%v：得到 %v，应为 %v", c.env, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	for s, want := range map[string]Lang{"zh": ZH, "en": EN, "EN": EN} {
		if l, ok := Parse(s); !ok || l != want {
			t.Errorf("Parse(%q) = %v %v", s, l, ok)
		}
	}
	for _, s := range []string{"", "fr", "zh_CN"} {
		if _, ok := Parse(s); ok {
			t.Errorf("Parse(%q) 不该认", s)
		}
	}
}

func TestList(t *testing.T) {
	xs := strings.Split("a b c d", " ")
	if got := For(EN).List(xs, 3); got != "a, b, c (4 in total)" {
		t.Fatalf("got %q", got)
	}
	if got := For(ZH).List(xs[:2], 3); got != "a、b" {
		t.Fatalf("got %q", got)
	}
}

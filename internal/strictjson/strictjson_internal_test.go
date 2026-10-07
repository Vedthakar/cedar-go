package strictjson

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/cedar-policy/cedar-go/internal/testutil"
)

func keysOf(data []byte) []string {
	var keys []string
	forEachKey(data, func(key []byte) bool {
		keys = append(keys, string(key))
		return true
	})
	return keys
}

func TestForEachKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", ``, nil},
		{"space", " \t\r\n", nil},
		{"notObject", `["a"]`, nil},
		{"emptyObject", ` { } `, nil},
		{"keys", `{"a":1,"b":"x","c":null}`, []string{"a", "b", "c"}},
		{"whitespace", " {\n\t\"a\" :\r 1 ,\n \"b\" : 2 }", []string{"a", "b"}},
		{"duplicates", `{"a":1,"a":2}`, []string{"a", "a"}},
		{"nestedValues", `{"a":{"b":[1,{"c":"}]{["}],"d":{}},"e":[[],[{}]],"f":"\"},"}`, []string{"a", "e", "f"}},
		{"escapedKey", `{"\u0074ype":1,"a\"b":2,"\\":3}`, []string{"type", `a"b`, `\`}},
		{"unicodeKey", `{"\u212a":1,"ſ":2}`, []string{"\u212a", "ſ"}},
		{"invalidUTF8Key", "{\"\xf5\":1}", []string{"\ufffd"}},

		// Malformed input stops the scan; json.Unmarshal reports the error afterwards.
		{"nonStringKey", `{"a":1,2:3}`, []string{"a"}},
		{"unterminatedKey", `{"a":1,"b`, []string{"a"}},
		{"unterminatedKeyAtEnd", `{"`, nil},
		{"badEscapeInKey", `{"a":1,"\x":2}`, []string{"a"}},
		{"missingColon", `{"a" 1,"b":2}`, []string{"a"}},
		{"keyAtEnd", `{"a"`, []string{"a"}},
		{"unterminatedValueString", `{"a":"b,"c":1`, []string{"a"}},
		{"unterminatedValue", `{"a":[1,2`, []string{"a"}},
		{"mismatchedBracket", `{"a":1],"b":2}`, []string{"a"}},
		{"trailingComma", `{"a":1,}`, []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testutil.Equals(t, keysOf([]byte(tt.in)), tt.want)
		})
	}
}

func TestForEachKeyStops(t *testing.T) {
	t.Parallel()
	var keys []string
	forEachKey([]byte(`{"a":1,"b":2,"c":3}`), func(key []byte) bool {
		keys = append(keys, string(key))
		return string(key) != "b"
	})
	testutil.Equals(t, keys, []string{"a", "b"})
}

// FuzzForEachKey checks that, for any valid JSON object, forEachKey finds the same keys that
// encoding/json does, and that it never panics on arbitrary input.
func FuzzForEachKey(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"a":1,"b":[true,false,null],"c":{"d":"e"}}`,
		`{"\u0041":"\"","b\\":"\\","c":"}{][,:"}`,
		` { "a" : [ { "b" : [ ] } ] , "c" : -1.5e3 } `,
		`{"__entity":{"type":"User","id":"alice"}}`,
		`{"a":"\ud83d\ude00","\ud83d\ude00":1}`,
		`{"a":1,`,
		`[1,2]`,
		`"string"`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got := keysOf(data)
		var m map[string]json.RawMessage
		if json.Unmarshal(data, &m) != nil || m == nil {
			return
		}
		want := make([]string, 0, len(m))
		for k := range m {
			want = append(want, k)
		}
		slices.Sort(want)
		slices.Sort(got)
		got = slices.Compact(got)
		testutil.FatalIf(t, !slices.Equal(got, want), "got %q want %q", got, want)
	})
}

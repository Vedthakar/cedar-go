package strictjson_test

import (
	"encoding/json"
	"testing"

	"github.com/cedar-policy/cedar-go/internal/strictjson"
	"github.com/cedar-policy/cedar-go/internal/testutil"
)

type target struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Untagged string
	Skipped  string `json:"-"`
	Dash     string `json:"-,"` //nolint:staticcheck // "-," names the field "-", as nodeJSON's Subtract does
	private  string //nolint:unused // encoding/json ignores unexported fields, so must strictjson
}

func TestUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    target
		wantErr error
	}{
		{"exact", `{"type":"T","id":"i"}`, target{Type: "T", ID: "i"}, nil},
		{"exactUntagged", `{"Untagged":"u"}`, target{Untagged: "u"}, nil},
		{"exactDash", `{"-":"d"}`, target{Dash: "d"}, nil},
		{"unknownKeysIgnored", `{"type":"T","other":1,"Skipped":"s","private":"p"}`, target{Type: "T"}, nil},
		{"leadingSpace", " \t\r\n{\"type\":\"T\"}", target{Type: "T"}, nil},
		{"null", `null`, target{}, nil},
		{"miscasedTag", `{"TYPE":"T","id":"i"}`, target{}, strictjson.ErrMiscasedKey},
		{"miscasedTagWithOptions", `{"type":"T","Id":"i"}`, target{}, strictjson.ErrMiscasedKey},
		{"miscasedUntagged", `{"untagged":"u"}`, target{}, strictjson.ErrMiscasedKey},
		{"miscasedAlongsideExact", `{"type":"T","Type":"U"}`, target{}, strictjson.ErrMiscasedKey},
		{"miscasedEscapedKey", `{"\u0054ype":"T"}`, target{}, strictjson.ErrMiscasedKey},
		{"miscasedAfterNestedValues", `{"other":{"type":["}"]},"ID":"i"}`, target{}, strictjson.ErrMiscasedKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got target
			err := strictjson.Unmarshal([]byte(tt.in), &got)
			testutil.ErrorIs(t, err, tt.wantErr)
			testutil.Equals(t, got, tt.want)
		})
	}
}

func TestUnmarshalUnicodeFold(t *testing.T) {
	t.Parallel()
	// encoding/json folds "\u017f" (LATIN SMALL LETTER LONG S) onto "s", so it must be rejected too.
	type s struct {
		S string `json:"s"`
	}
	var got s
	err := strictjson.Unmarshal([]byte("{\"\u017f\":\"x\"}"), &got)
	testutil.ErrorIs(t, err, strictjson.ErrMiscasedKey)
}

func TestUnmarshalErrorsMatchEncodingJSON(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`"string"`, `[]`, `42`, `{`, `{"type":`, `{"type":1}`, ``} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			var want, got target
			wantErr := json.Unmarshal([]byte(in), &want)
			testutil.Error(t, wantErr)
			err := strictjson.Unmarshal([]byte(in), &got)
			testutil.Equals(t, err.Error(), wantErr.Error())
		})
	}
}

func TestHasUnknownKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"exact", `{"type":"T","id":"i","Untagged":"u","-":"d"}`, false},
		{"empty", `{}`, false},
		{"unknown", `{"type":"T","other":1}`, true},
		{"miscased", `{"Type":"T"}`, true},
		{"skipped", `{"Skipped":"s"}`, true},
		{"unexported", `{"private":"p"}`, true},
		{"notObject", `["other"]`, false},
		// Input is not validated; json.Unmarshal reports malformed input when it is decoded.
		{"malformedKnown", `{"type"`, false},
		{"malformedUnknown", `{"other"`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testutil.Equals(t, strictjson.HasUnknownKeys([]byte(tt.in), &target{}), tt.want)
		})
	}
}

func TestEmbeddedFieldPanics(t *testing.T) {
	t.Parallel()
	type embedded struct {
		target
	}
	testutil.Panic(t, func() {
		_ = strictjson.Unmarshal([]byte(`{}`), &embedded{})
	})
}

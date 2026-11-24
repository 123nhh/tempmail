package handler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestNormalizeContactEmail(t *testing.T) {
	// 地址由片段拼接，便于阅读各用例意图。
	at := func(local, domain string) string { return local + "@" + domain }
	valid := map[string]string{
		"":                        "",
		"   ":                     "",
		at("user", "example.com"): at("user", "example.com"),
		"  " + at("User.Name", "Example.COM") + "  ": at("user.name", "example.com"),
		at("first+tag", "sub.example.co.uk"):         at("first+tag", "sub.example.co.uk"),
	}
	for in, want := range valid {
		got, err := normalizeContactEmail(in)
		if err != nil || got != want {
			t.Errorf("normalizeContactEmail(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}

	invalid := []string{
		"plainaddress",
		at("user", "localhost"),
		at("user", "example..com"),
		"Alice <" + at("alice", "example.com") + ">",
		"<" + at("alice", "example.com") + ">",
		at("a b", "example.com"),
		at("a", "exa mple.com"),
		at("", "example.com"),
		at("user", ""),
		at("user", "-bad.com"),
		at("user", "bad-.com"),
		at("user", "exa_mple.com"),
		at(`"quoted"`, "example.com"),
		at("a", longLabelDomain()),
	}
	for _, in := range invalid {
		if got, err := normalizeContactEmail(in); err == nil {
			t.Errorf("normalizeContactEmail(%q) = %q, nil; want error", in, got)
		}
	}
}

func longLabelDomain() string {
	b := make([]byte, 0, 300)
	for len(b) < 260 {
		b = append(b, "abcdefghij."...)
	}
	return string(b) + "com"
}

func TestIsUniqueViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pg unique", &pgconn.PgError{Code: "23505"}, true},
		{"wrapped pg unique", fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505"}), true},
		{"pg fk violation", &pgconn.PgError{Code: "23503", Message: "violates foreign key"}, false},
		{"pg other mentioning unique", &pgconn.PgError{Code: "42P01", Message: "unique table missing"}, false},
		{"plain duplicate", errors.New("duplicate key value"), true},
		{"plain unrelated", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		if got := isUniqueViolation(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

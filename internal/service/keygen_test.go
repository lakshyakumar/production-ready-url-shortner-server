package service

// Internal-package tests for shortKeyFromURL. Lives in `package service`
// (not service_test) so it can reach the unexported function and constants.

import (
	"strings"
	"testing"
)

func TestShortKeyFromURL_LengthAndAlphabet(t *testing.T) {
	inputs := []string{
		"https://example.com",
		"https://example.com/very/long/path?with=lots&of=params",
		"a",
		"",
		strings.Repeat("x", 10000),
		"https://example.com/路径?查询=值", // unicode
	}
	for _, in := range inputs {
		key := shortKeyFromURL(in)
		if len(key) != shortKeyLength {
			t.Errorf("input %q: len=%d want %d", in, len(key), shortKeyLength)
		}
		for i, r := range key {
			if !strings.ContainsRune(base62Alphabet, r) {
				t.Errorf("input %q: char %d (%q) not in base62 alphabet", in, i, r)
			}
		}
	}
}

func TestShortKeyFromURL_Deterministic(t *testing.T) {
	url := "https://example.com/some/path"
	first := shortKeyFromURL(url)
	for i := 0; i < 100; i++ {
		if got := shortKeyFromURL(url); got != first {
			t.Fatalf("non-deterministic: iteration %d got %q want %q", i, got, first)
		}
	}
}

func TestShortKeyFromURL_DifferentInputsDifferentKeys(t *testing.T) {
	// Not a strict guarantee (collisions exist by definition), but for
	// these clearly-different inputs we'd need cosmically bad luck.
	pairs := [][2]string{
		{"https://example.com/a", "https://example.com/b"},
		{"https://example.com", "https://example.org"},
		{"foo", "bar"},
	}
	for _, p := range pairs {
		if shortKeyFromURL(p[0]) == shortKeyFromURL(p[1]) {
			t.Errorf("unexpected collision: %q vs %q", p[0], p[1])
		}
	}
}

// FuzzShortKeyFromURL probes the hash-and-encode pipeline with arbitrary
// byte input. The function is total — no panic for any input — and must
// always emit a fixed-length, base62-clean string.
//
// Run:
//
//	go test ./internal/service -run=^$ -fuzz=FuzzShortKeyFromURL -fuzztime=20s
func FuzzShortKeyFromURL(f *testing.F) {
	// Seed corpus: a few representative inputs so the fuzzer has something
	// to mutate. Failed fuzz cases land in testdata/fuzz/FuzzShortKeyFromURL/.
	for _, seed := range []string{
		"",
		"https://example.com",
		"https://example.com/path?q=x",
		"\x00",
		strings.Repeat("a", 1000),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		key := shortKeyFromURL(input)

		if len(key) != shortKeyLength {
			t.Fatalf("len=%d want %d (input %q)", len(key), shortKeyLength, input)
		}
		for i, r := range key {
			if !strings.ContainsRune(base62Alphabet, r) {
				t.Fatalf("char %d (%q) not in base62 (input %q)", i, r, input)
			}
		}
		// Determinism is part of the contract.
		if again := shortKeyFromURL(input); again != key {
			t.Fatalf("non-deterministic: first=%q second=%q (input %q)", key, again, input)
		}
	})
}

// BenchmarkShortKeyFromURL measures the cost of one hash-and-base62 round.
// Sub-microsecond expected; if it ever crosses 1µs there's been a regression
// in crypto/sha256 or math/big handling for this codepath.
func BenchmarkShortKeyFromURL(b *testing.B) {
	url := "https://example.com/some/reasonably-long/path?with=query&and=stuff"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shortKeyFromURL(url)
	}
}

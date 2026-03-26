package main

import "testing"

func TestParseOwnedShards(t *testing.T) {
	shards, err := parseOwnedShards("1, 3,3, 5")
	if err != nil {
		t.Fatalf("parseOwnedShards() error = %v", err)
	}
	if len(shards) != 3 || shards[0] != 1 || shards[1] != 3 || shards[2] != 5 {
		t.Fatalf("parseOwnedShards() = %v, want [1 3 5]", shards)
	}
}

func TestParseOwnedShards_RejectsOutOfRange(t *testing.T) {
	if _, err := parseOwnedShards("99"); err == nil {
		t.Fatal("expected out of range error")
	}
}

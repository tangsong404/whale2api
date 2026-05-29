package account

import (
	"testing"

	"whale2api/internal/config"
)

func TestMemoryLookupAccountsAndFind(t *testing.T) {
	a1 := config.Account{Email: "a@b.com", Token: "t1"}
	a2 := config.Account{Email: "c@d.com", Token: "t2"}
	m := NewMemoryLookup([]config.Account{a1, a2})
	if len(m.Accounts()) != 2 {
		t.Fatalf("accounts len got %d", len(m.Accounts()))
	}
	got, ok := m.FindAccount("a@b.com")
	if !ok || got.Token != "t1" {
		t.Fatalf("find a: ok=%v token=%q", ok, got.Token)
	}
}

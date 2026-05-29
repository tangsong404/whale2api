package pooldb

import "testing"

func TestAccountToConfigEmail(t *testing.T) {
	acc := AccountToConfig("u@example.com", "pass")
	if acc.Email != "u@example.com" || acc.Mobile != "" {
		t.Fatalf("got %+v", acc)
	}
}

func TestAccountToConfigMobile(t *testing.T) {
	acc := AccountToConfig("13800138000", "pass")
	if acc.Mobile != "13800138000" || acc.Email != "" {
		t.Fatalf("got %+v", acc)
	}
}

package config

import (
	"strings"
	"testing"
)

func TestClientIPDefaults(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.ClientIP.None || len(c.ClientIP.Trusted) != 0 || c.ClientIP.Header != HeaderXForwardedFor {
		t.Errorf("défauts inattendus : %+v", c.ClientIP)
	}
}

func TestClientIPValues(t *testing.T) {
	c, err := Load(env(with(VarTrustedProxies, "10.0.0.0/8, 192.0.2.1,2001:db8::/32", VarClientIPHeader, "cf-connecting-ip")))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ClientIP.Trusted) != 3 || c.ClientIP.Trusted[1].String() != "192.0.2.1/32" || c.ClientIP.Header != HeaderCFConnecting {
		t.Errorf("valeurs inattendues : %+v", c.ClientIP)
	}
	c, err = Load(env(with(VarTrustedProxies, "NONE")))
	if err != nil || !c.ClientIP.None {
		t.Errorf("none : %+v %v", c.ClientIP, err)
	}
}

func TestClientIPInvalid(t *testing.T) {
	for _, v := range []string{"abc", "10.0.0.0/99", "10.0.0.1,,10.0.0.2", "none,10.0.0.1"} {
		msg := mustFail(t, with(VarTrustedProxies, v), VarTrustedProxies)
		if !strings.Contains(msg, "TRUSTED_PROXIES") {
			t.Error(msg)
		}
	}
	mustFail(t, with(VarClientIPHeader, "X-Evil"), VarClientIPHeader)
}

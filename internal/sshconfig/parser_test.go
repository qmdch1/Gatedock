package sshconfig

import (
	"strings"
	"testing"
)

func TestConfigParser(t *testing.T) {
	p, e := Parse(strings.NewReader(`Host bastion
 HostName 127.0.0.1
 Port=6022
 User ec2-user
 IdentityFile "C:\Users\dev\.ssh\stg key.pem"
Host pipeline internal
 HostName 10.0.0.2
 ProxyJump bastion
 User developer
 IdentityFile ~/.ssh/id_ed25519
Host *
 Port 22
 ServerAliveInterval 30
`))
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Hosts) != 3 || p.Hosts[0].Port != 6022 || p.Hosts[1].Port != 22 || p.Hosts[1].Jumps[0] != "bastion" || p.Hosts[0].IdentityFile != `C:\Users\dev\.ssh\stg key.pem` {
		t.Fatalf("%+v", p)
	}
	if !p.Hosts[1].Valid {
		t.Fatal(p.Hosts[1])
	}
}
func TestParserRejectsUnsafeUnsupported(t *testing.T) {
	p, e := Parse(strings.NewReader("Host unsafe\n User dev\n IdentityFile ~/.ssh/id\n ProxyCommand nc %h %p\nHost missing\n Port 99999\nInclude ~/.ssh/extra\nMatch exec touch-hacked\n User ignored\n"))
	if e != nil {
		t.Fatal(e)
	}
	if p.Hosts[0].Valid || p.Hosts[1].Valid || len(p.Warnings) < 2 {
		t.Fatalf("%+v", p)
	}
	if _, e = Parse(strings.NewReader("Host \"broken")); e == nil {
		t.Fatal("unclosed quote accepted")
	}
}
func TestFirstValueAndNegatedPattern(t *testing.T) {
	p, e := Parse(strings.NewReader("Host * !excluded\n User first\n IdentityFile ~/.ssh/id\nHost app\n User later\nHost excluded\n User explicit\n IdentityFile ~/.ssh/other\n"))
	if e != nil {
		t.Fatal(e)
	}
	if p.Hosts[0].Username != "first" || p.Hosts[1].Username != "explicit" {
		t.Fatalf("%+v", p)
	}
}

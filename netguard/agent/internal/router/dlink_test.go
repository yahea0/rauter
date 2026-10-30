package router

import (
    "encoding/base64"
    "testing"

    "github.com/yahea0/netguard/agent/internal/config"
)

func TestXmlTag(t *testing.T) {
    s := `<LoginResponse><LoginResult>OK</LoginResult><Challenge>abc123</Challenge></LoginResponse>`
    if got := xmlTag(s, "Challenge"); got != "abc123" {
        t.Fatalf("xmlTag(Challenge) = %q, نريد abc123", got)
    }
    if got := xmlTag(s, "Missing"); got != "" {
        t.Fatalf("وسم مفقود يجب أن يعيد نصاً فارغاً، حصلنا %q", got)
    }
}

func TestHmacMD5Deterministic(t *testing.T) {
    a := hmacMD5("challenge123pass", "publickey456")
    b := hmacMD5("challenge123pass", "publickey456")
    if a == "" || a != b {
        t.Fatalf("HMAC-MD5 يجب أن يكون ثابتاً وغير فارغ: %q vs %q", a, b)
    }
    if len(a) != 32 {
        t.Fatalf("طول بصمة MD5 السداسية يجب أن يكون 32، حصلنا %d", len(a))
    }
}

func TestRender(t *testing.T) {
    c := New(config.Router{Username: "admin", Password: "s3cret"})
    want := "u=admin&p=s3cret&b=" + base64.StdEncoding.EncodeToString([]byte("s3cret"))
    if got := c.render("u={{username}}&p={{password}}&b={{password_b64}}"); got != want {
        t.Fatalf("render = %q, نريد %q", got, want)
    }
}

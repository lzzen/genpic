package refimages

import "testing"

func TestParseEmpty(t *testing.T) {
	got, err := Parse(nil)
	if err != nil || got != nil {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestParseOne(t *testing.T) {
	got, err := Parse([]Input{{MIMEType: "image/png", B64JSON: "iVBORw0KGgo="}})
	if err != nil || len(got) != 1 || got[0].MIMEType != "image/png" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestParseTooMany(t *testing.T) {
	in := make([]Input, 7)
	for i := range in {
		in[i] = Input{B64JSON: "QQ==", MIMEType: "image/png"}
	}
	_, err := Parse(in)
	if err == nil {
		t.Fatal("want error")
	}
}

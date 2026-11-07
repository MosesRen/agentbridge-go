package claude

import (
	"strings"
	"testing"
)

func TestNativeSessionResumeOptions(t *testing.T) {
	for _, test := range []struct {
		name    string
		options Options
		want    string
	}{
		{"resume", Options{Resume: "session-id"}, "--resume=session-id"},
		{"continue", Options{Continue: true}, "--continue"},
		{"session ID", Options{SessionID: "session-id"}, "--session-id=session-id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, err := test.options.normalized()
			if err != nil {
				t.Fatal(err)
			}
			args := "\n" + strings.Join(options.args(), "\n") + "\n"
			if !strings.Contains(args, "\n"+test.want+"\n") {
				t.Fatalf("missing %s: %v", test.want, options.args())
			}
			for _, unsupported := range []string{"--session-mirror", "--await-claim", "--replay-user-messages"} {
				if strings.Contains(args, unsupported) {
					t.Fatalf("unexpected advanced flag %s", unsupported)
				}
			}
		})
	}
	for _, options := range []Options{{Resume: "id", Continue: true}, {Resume: "id", SessionID: "id"}, {Continue: true, SessionID: "id"}} {
		if _, err := options.normalized(); err == nil {
			t.Fatal("conflicting session startup options accepted")
		}
	}
}

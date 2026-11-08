package workbench

import (
	"context"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/desktop"
)

func TestVNCExplicitEmptyUsernameKeepsItsPasswordSeparate(t *testing.T) {
	answers := make(chan [2]string, 1)
	state, host := realVNCState(t, "", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		user, password, err := request.AskCredentials()
		if err != nil {
			return nil, err
		}
		answers <- [2]string{user, password}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	legacy := hostSecretName(host.CatalogID, hostPasswordPart)
	state.secrets.vault.Set(legacy, "legacy password")
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "VNC credentials", func() bool { return state.modal == modalPassword })
	if len(state.passwordFields) != 2 || state.passwordFields[1].String() != "" {
		t.Fatal("the legacy password prefilled an account-based login")
	}
	state.passwordFields[1] = *text.NewEditor("account password")
	state.secrets.save = true
	state.Click(hitPasswordSubmit)
	waitUntil(t, state, "empty-user desktop", func() bool { return session.desk != nil })
	if got := <-answers; got != [2]string{"", "account password"} {
		t.Fatalf("submitted %q", got)
	}
	if got, ok := vaultValue(t, state, hostSecretName(host.CatalogID, vncSystemPasswordPart(""))); !ok || got != "account password" {
		t.Fatal("empty-account password was not saved separately")
	}
	if got, ok := vaultValue(t, state, legacy); !ok || got != "legacy password" {
		t.Fatal("the empty-account password changed the legacy secret")
	}
}

func TestVNCEmptyPasswordKeepsPromptOpen(t *testing.T) {
	for _, test := range []struct {
		name, user string
		account    bool
	}{
		{"known account", "alice", true},
		{"empty account", "", true},
		{"VNC password", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			answers := make(chan string, 1)
			state, _ := realVNCState(t, test.user, func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
				var password string
				var err error
				if test.account {
					_, password, err = request.AskCredentials()
				} else {
					password, err = request.Ask()
				}
				if err != nil {
					return nil, err
				}
				answers <- password
				return &encryptedVNC{newPictureOnly()}, nil
			})
			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "VNC password prompt", func() bool { return state.modal == modalPassword })
			state.Click(hitPasswordSubmit)
			if state.modal != modalPassword || state.passwordError == "" || session.desk != nil {
				t.Fatal("empty password closed the VNC prompt")
			}
			select {
			case <-answers:
				t.Fatal("empty password reached connection")
			default:
			}
			index := passwordQuestionIndex(state.passwordQuestions)
			state.passwordFields[index] = *text.NewEditor("correct password")
			state.Click(hitPasswordSubmit)
			waitUntil(t, state, "VNC desktop after correction", func() bool { return session.desk != nil })
			if got := <-answers; got != "correct password" {
				t.Fatalf("password=%q", got)
			}
		})
	}
}

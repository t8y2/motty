package fidoaskpass

import (
	"encoding/json"
	"os"
	"testing"
)

func TestAskpassRoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "motty-ask-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, address, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	received := make(chan Request, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		var request Request
		if err := json.NewDecoder(connection).Decode(&request); err != nil {
			return
		}
		received <- request
		_ = json.NewEncoder(connection).Encode(Response{OK: true, Response: "123456"})
	}()

	connection, err := Dial(address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := json.NewEncoder(connection).Encode(Request{Token: "token", Prompt: "Enter PIN"}); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Response != "123456" {
		t.Fatalf("response = %+v", response)
	}
	request := <-received
	if request.Token != "token" || request.Prompt != "Enter PIN" {
		t.Fatalf("request = %+v", request)
	}
}

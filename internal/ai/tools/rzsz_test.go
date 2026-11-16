package tools

import "testing"

func TestRZSZDecoders(t *testing.T) {
	up, err := DecodeRZSZUpload([]byte(`{"local_path":"C:\\a.txt","remote_dir":"/tmp","overwrite":true}`))
	if err != nil || up.LocalPath != `C:\a.txt` || up.RemoteDir != "/tmp" || !up.Overwrite || up.TimeoutSeconds != DefaultTimeoutSeconds {
		t.Fatalf("%+v %v", up, err)
	}
	if _, err := DecodeRZSZUpload([]byte(`{"local_path":"ok","remote_dir":"bad\n"}`)); err == nil {
		t.Fatal("a control character was accepted")
	}
	if _, err := DecodeRZSZUpload([]byte(`{}`)); err == nil {
		t.Fatal("missing local_path was accepted")
	}
	down, err := DecodeRZSZDownload([]byte(`{"remote_path":"/tmp/a b","local_dir":"D:\\box","timeout_seconds":10}`))
	if err != nil || down.RemotePath != "/tmp/a b" || down.LocalDir != `D:\box` || down.TimeoutSeconds != 10 || down.Request().Upload {
		t.Fatalf("%+v %v", down, err)
	}
	if _, err := DecodeRZSZDownload([]byte(`{"remote_path":"/tmp/a","local_dir":"D:\\box","extra":1}`)); err == nil {
		t.Fatal("an unknown field was accepted")
	}
	status, err := DecodeRZSZStatus([]byte(`{"session":"w1:2"}`))
	if err != nil || status.Session != "w1:2" {
		t.Fatalf("%+v %v", status, err)
	}
	if _, err := DecodeRZSZStatus([]byte(`{"session":"ok","extra":1}`)); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}

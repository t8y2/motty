//go:build darwin && cgo

package sshconn

/*
#cgo LDFLAGS: -framework GSS
#include <GSS/GSS.h>
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"fmt"
	"unsafe"

	"golang.org/x/crypto/ssh"
)

// KerberosAvailable reports whether the build can use macOS system credentials.
const KerberosAvailable = true

func (conn *Conn) kerberosAuth(host string) ssh.AuthMethod {
	return ssh.GSSAPIWithMICAuthMethod(&kerberosClient{conn: conn}, host)
}

type kerberosClient struct {
	conn *Conn
	ctx  C.gss_ctx_id_t
}

func (client *kerberosClient) InitSecContext(target string, token []byte, _ bool) ([]byte, bool, error) {
	client.conn.method("gssapi-with-mic")
	var minor C.OM_uint32
	var name C.gss_name_t
	targetBytes := C.CBytes([]byte(target))
	defer C.free(targetBytes)
	targetBuffer := C.gss_buffer_desc{length: C.size_t(len(target)), value: targetBytes}
	status := C.gss_import_name(&minor, &targetBuffer, C.GSS_C_NT_HOSTBASED_SERVICE, &name)
	if status != C.GSS_S_COMPLETE {
		return nil, false, kerberosError("import service name", status, minor)
	}
	defer C.gss_release_name(&minor, &name)

	var input C.gss_buffer_desc
	if len(token) != 0 {
		input = C.gss_buffer_desc{length: C.size_t(len(token)), value: C.CBytes(token)}
		defer C.free(input.value)
	}
	var output C.gss_buffer_desc
	// Request integrity and mutual authentication without credential delegation.
	status = C.gss_init_sec_context(&minor, nil, &client.ctx, name, C.GSS_KRB5_MECHANISM,
		C.GSS_C_MUTUAL_FLAG|C.GSS_C_INTEG_FLAG, 0, nil, &input, nil, &output, nil, nil)
	defer C.gss_release_buffer(&minor, &output)
	if status != C.GSS_S_COMPLETE && status != C.GSS_S_CONTINUE_NEEDED {
		return nil, false, kerberosError("initialize security context", status, minor)
	}
	return copyGSSBuffer(output), status == C.GSS_S_CONTINUE_NEEDED, nil
}

func (client *kerberosClient) GetMIC(message []byte) ([]byte, error) {
	var minor C.OM_uint32
	input := C.gss_buffer_desc{length: C.size_t(len(message)), value: C.CBytes(message)}
	defer C.free(input.value)
	var output C.gss_buffer_desc
	status := C.gss_get_mic(&minor, client.ctx, C.GSS_C_QOP_DEFAULT, &input, &output)
	defer C.gss_release_buffer(&minor, &output)
	if status != C.GSS_S_COMPLETE {
		return nil, kerberosError("sign SSH authentication", status, minor)
	}
	return copyGSSBuffer(output), nil
}

func (client *kerberosClient) DeleteSecContext() error {
	if client.ctx == nil {
		return nil
	}
	var minor C.OM_uint32
	status := C.gss_delete_sec_context(&minor, &client.ctx, nil)
	if status != C.GSS_S_COMPLETE {
		return kerberosError("release security context", status, minor)
	}
	return nil
}

func copyGSSBuffer(buffer C.gss_buffer_desc) []byte {
	return bytes.Clone(unsafe.Slice((*byte)(buffer.value), int(buffer.length)))
}

func kerberosError(operation string, major, minor C.OM_uint32) error {
	var status, next C.OM_uint32
	var message C.gss_buffer_desc
	if minor != 0 && C.gss_display_status(&status, minor, C.GSS_C_MECH_CODE, C.GSS_KRB5_MECHANISM, &next, &message) == C.GSS_S_COMPLETE {
		defer C.gss_release_buffer(&status, &message)
		return fmt.Errorf("Kerberos %s: %s", operation, copyGSSBuffer(message))
	}
	return fmt.Errorf("Kerberos %s: GSSAPI status %#x/%#x", operation, uint32(major), uint32(minor))
}

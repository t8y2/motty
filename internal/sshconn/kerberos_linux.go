//go:build linux

package sshconn

import (
	"bytes"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/crypto/ssh"
)

// KerberosAvailable reports whether the build can use system Kerberos
// credentials. The library is loaded when a host first needs it, so a system
// without libgssapi_krb5 fails that connection with a clear error.
const KerberosAvailable = true

// gssLibrary is the system GSS-API library, MIT or Heimdal, which Linux
// distributions ship. It is loaded at run time, so the build needs no headers
// and no cgo.
const gssLibrary = "libgssapi_krb5.so.2"

// GSS-API constants from gssapi.h.
const (
	gssStatusComplete       = 0
	gssStatusContinueNeeded = 1
	gssStatusMechCode       = 2
	gssFlagMutual           = 2
	gssFlagInteg            = 4
	gssQopDefault           = 0
	gssKrb5Mechanism        = "1.2.840.113554.1.2.2"
	gssHostbasedService     = "1.2.840.113554.1.2.1.4"
)

// gssBuffer is gss_buffer_desc. Input buffers point into Go memory, which the
// library only reads during a call; output buffers point into library memory
// and are released with gss_release_buffer.
type gssBuffer struct {
	length uintptr
	value  unsafe.Pointer
}

// gssFuncs are the entry points resolved from the library. Each takes the minor
// status first, as the C API does.
type gssFuncs struct {
	strToOID         func(minor *uint32, str *gssBuffer, oid *uintptr) uint32
	releaseOID       func(minor *uint32, oid *uintptr) uint32
	importName       func(minor *uint32, name *gssBuffer, nameType uintptr, out *uintptr) uint32
	releaseName      func(minor *uint32, name *uintptr) uint32
	initSecContext   func(minor *uint32, cred unsafe.Pointer, ctx *uintptr, target uintptr, mech uintptr, flags uint32, timeReq uint32, bindings unsafe.Pointer, input *gssBuffer, actualMech unsafe.Pointer, output *gssBuffer, retFlags unsafe.Pointer, timeRec unsafe.Pointer) uint32
	getMIC           func(minor *uint32, ctx uintptr, qop uint32, message *gssBuffer, token *gssBuffer) uint32
	deleteSecContext func(minor *uint32, ctx *uintptr, token unsafe.Pointer) uint32
	releaseBuffer    func(minor *uint32, buffer *gssBuffer) uint32
	displayStatus    func(minor *uint32, status uint32, statusType int32, mech uintptr, context *uint32, text *gssBuffer) uint32
}

var (
	gssOnce sync.Once
	gss     gssFuncs
	gssErr  error
)

// loadGSS opens the system library once and binds the functions above. A
// missing symbol is an error, not a panic, because purego panics on it.
func loadGSS() (*gssFuncs, error) {
	gssOnce.Do(func() {
		handle, err := purego.Dlopen(gssLibrary, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gssErr = fmt.Errorf("Kerberos needs %s: %w", gssLibrary, err)
			return
		}
		bind := func(name string, fptr any) {
			if gssErr != nil {
				return
			}
			sym, err := purego.Dlsym(handle, name)
			if err != nil {
				gssErr = fmt.Errorf("Kerberos needs %s in %s: %w", name, gssLibrary, err)
				return
			}
			purego.RegisterFunc(fptr, sym)
		}
		bind("gss_str_to_oid", &gss.strToOID)
		bind("gss_release_oid", &gss.releaseOID)
		bind("gss_import_name", &gss.importName)
		bind("gss_release_name", &gss.releaseName)
		bind("gss_init_sec_context", &gss.initSecContext)
		bind("gss_get_mic", &gss.getMIC)
		bind("gss_delete_sec_context", &gss.deleteSecContext)
		bind("gss_release_buffer", &gss.releaseBuffer)
		bind("gss_display_status", &gss.displayStatus)
	})
	return &gss, gssErr
}

func (conn *Conn) kerberosAuth(host string) ssh.AuthMethod {
	return ssh.GSSAPIWithMICAuthMethod(&kerberosClient{conn: conn}, host)
}

type kerberosClient struct {
	conn *Conn
	ctx  uintptr
}

func (client *kerberosClient) InitSecContext(target string, token []byte, _ bool) ([]byte, bool, error) {
	client.conn.method("gssapi-with-mic")
	lib, err := loadGSS()
	if err != nil {
		return nil, false, err
	}
	var minor uint32
	mech, err := gssOID(lib, gssKrb5Mechanism)
	if err != nil {
		return nil, false, err
	}
	defer lib.releaseOID(&minor, &mech)
	nameType, err := gssOID(lib, gssHostbasedService)
	if err != nil {
		return nil, false, err
	}
	defer lib.releaseOID(&minor, &nameType)

	targetBuffer := bufferOf([]byte(target))
	var name uintptr
	status := lib.importName(&minor, &targetBuffer, nameType, &name)
	if status != gssStatusComplete {
		return nil, false, kerberosError(lib, "import service name", status, minor)
	}
	defer lib.releaseName(&minor, &name)

	input := bufferOf(token)
	var output gssBuffer
	// Request integrity and mutual authentication without credential delegation.
	status = lib.initSecContext(&minor, nil, &client.ctx, name, mech, gssFlagMutual|gssFlagInteg, 0, nil,
		&input, nil, &output, nil, nil)
	defer lib.releaseBuffer(&minor, &output)
	if status != gssStatusComplete && status != gssStatusContinueNeeded {
		return nil, false, kerberosError(lib, "initialize security context", status, minor)
	}
	return copyGSSBuffer(output), status == gssStatusContinueNeeded, nil
}

func (client *kerberosClient) GetMIC(message []byte) ([]byte, error) {
	lib, err := loadGSS()
	if err != nil {
		return nil, err
	}
	var minor uint32
	input := bufferOf(message)
	var output gssBuffer
	status := lib.getMIC(&minor, client.ctx, gssQopDefault, &input, &output)
	defer lib.releaseBuffer(&minor, &output)
	if status != gssStatusComplete {
		return nil, kerberosError(lib, "sign SSH authentication", status, minor)
	}
	return copyGSSBuffer(output), nil
}

func (client *kerberosClient) DeleteSecContext() error {
	if client.ctx == 0 {
		return nil
	}
	lib, err := loadGSS()
	if err != nil {
		return err
	}
	var minor uint32
	status := lib.deleteSecContext(&minor, &client.ctx, nil)
	if status != gssStatusComplete {
		return kerberosError(lib, "release security context", status, minor)
	}
	return nil
}

// gssOID turns a dotted object identifier into a GSS-API OID with the standard
// gss_str_to_oid, so MIT and Heimdal both work. The caller releases it.
func gssOID(lib *gssFuncs, dotted string) (uintptr, error) {
	var minor uint32
	var oid uintptr
	text := bufferOf([]byte(dotted))
	status := lib.strToOID(&minor, &text, &oid)
	if status != gssStatusComplete {
		return 0, kerberosError(lib, "parse object identifier "+dotted, status, minor)
	}
	return oid, nil
}

// bufferOf describes data as a gss_buffer_desc without copying it. The
// unsafe.Pointer field keeps the data alive for as long as the buffer is used.
func bufferOf(data []byte) gssBuffer {
	if len(data) == 0 {
		return gssBuffer{}
	}
	return gssBuffer{length: uintptr(len(data)), value: unsafe.Pointer(&data[0])}
}

func copyGSSBuffer(buffer gssBuffer) []byte {
	if buffer.length == 0 {
		return nil
	}
	return bytes.Clone(unsafe.Slice((*byte)(buffer.value), int(buffer.length)))
}

func kerberosError(lib *gssFuncs, operation string, major, minor uint32) error {
	if minor != 0 {
		var status, next uint32
		var message gssBuffer
		if lib.displayStatus(&status, minor, gssStatusMechCode, 0, &next, &message) == gssStatusComplete {
			defer lib.releaseBuffer(&status, &message)
			return fmt.Errorf("Kerberos %s: %s", operation, copyGSSBuffer(message))
		}
	}
	return fmt.Errorf("Kerberos %s: GSSAPI status %#x/%#x", operation, major, minor)
}

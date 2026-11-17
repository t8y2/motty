//go:build windows

package sshconn

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/crypto/ssh"
)

// KerberosAvailable reports whether the build can use system Kerberos
// credentials. Windows reaches them through SSPI, which the program loads when a
// host first needs it, so no cgo or Windows SDK is involved.
const KerberosAvailable = true

// SSPI constants from sspi.h.
const (
	sspiCredOutbound        = 0x2
	sspiNativeDataRep       = 0x10
	sspiReqMutualAuth       = 0x2
	sspiReqAllocateMemory   = 0x100
	sspiReqIntegrity        = 0x10000
	sspiBufferData          = 1
	sspiBufferToken         = 2
	sspiAttributeSizes      = 0
	sspiStatusOK            = 0
	sspiStatusContinue      = 0x90312
	sspiFormatFromSystem    = 0x1000
	sspiFormatIgnoreInserts = 0x200
)

// sspiHandle is SecHandle: a credential or context handle, two machine words.
type sspiHandle struct {
	lower uintptr
	upper uintptr
}

// sspiBuffer is SecBuffer. data points into Go memory for input and into SSPI's
// memory for output.
type sspiBuffer struct {
	length uint32
	kind   uint32
	data   unsafe.Pointer
}

// sspiBufferDesc is SecBufferDesc.
type sspiBufferDesc struct {
	version uint32
	count   uint32
	buffers unsafe.Pointer
}

// sspiSizes is SecPkgContext_Sizes.
type sspiSizes struct {
	maxToken        uint32
	maxSignature    uint32
	blockSize       uint32
	securityTrailer uint32
}

// sspiTimestamp is TimeStamp, the expiry SSPI fills in and this client ignores.
type sspiTimestamp [2]uint32

var (
	secur32                     = syscall.NewLazyDLL("secur32.dll")
	procAcquireCredentialsW     = secur32.NewProc("AcquireCredentialsHandleW")
	procInitializeSecContextW   = secur32.NewProc("InitializeSecurityContextW")
	procDeleteSecurityContext   = secur32.NewProc("DeleteSecurityContext")
	procFreeCredentialsHandle   = secur32.NewProc("FreeCredentialsHandle")
	procFreeContextBuffer       = secur32.NewProc("FreeContextBuffer")
	procQueryContextAttributesW = secur32.NewProc("QueryContextAttributesW")
	procMakeSignature           = secur32.NewProc("MakeSignature")
)

// kerberosClient is one SSPI Kerberos context for an SSH connection. Every
// buffer SSPI reads or writes lives in this struct rather than on the stack:
// the calls take addresses as uintptr, which does not keep a stack frame from
// moving, while a heap object's address is stable.
type kerberosClient struct {
	conn    *Conn
	cred    sspiHandle
	ctx     sspiHandle
	hasCred bool
	hasCtx  bool

	expiry     sspiTimestamp
	attributes uint32
	input      sspiBuffer
	inputDesc  sspiBufferDesc
	output     sspiBuffer
	outputDesc sspiBufferDesc

	sizes       sspiSizes
	sizeOnce    sync.Once
	sizeErr     error
	message     [2]sspiBuffer
	messageDesc sspiBufferDesc
}

func (conn *Conn) kerberosAuth(host string) ssh.AuthMethod {
	return ssh.GSSAPIWithMICAuthMethod(&kerberosClient{conn: conn}, host)
}

func (client *kerberosClient) InitSecContext(target string, token []byte, _ bool) ([]byte, bool, error) {
	client.conn.method("gssapi-with-mic")
	if !client.hasCred {
		if err := client.acquire(); err != nil {
			return nil, false, err
		}
	}
	spn, err := syscall.UTF16PtrFromString(sspiServiceName(target))
	if err != nil {
		return nil, false, err
	}
	var inputDesc uintptr
	if len(token) != 0 {
		client.input = sspiBuffer{length: uint32(len(token)), kind: sspiBufferToken, data: unsafe.Pointer(&token[0])}
		client.inputDesc = sspiBufferDesc{version: 0, count: 1, buffers: unsafe.Pointer(&client.input)}
		inputDesc = uintptr(unsafe.Pointer(&client.inputDesc))
	}
	// SSPI allocates the output token, so it is copied and released below.
	client.output = sspiBuffer{kind: sspiBufferToken}
	client.outputDesc = sspiBufferDesc{version: 0, count: 1, buffers: unsafe.Pointer(&client.output)}

	var contextHandle uintptr
	if client.hasCtx {
		contextHandle = uintptr(unsafe.Pointer(&client.ctx))
	}
	// Request mutual authentication and integrity, and no credential delegation.
	status, _, _ := procInitializeSecContextW.Call(
		uintptr(unsafe.Pointer(&client.cred)), contextHandle, uintptr(unsafe.Pointer(spn)),
		sspiReqMutualAuth|sspiReqIntegrity|sspiReqAllocateMemory, 0, sspiNativeDataRep,
		inputDesc, 0, uintptr(unsafe.Pointer(&client.ctx)), uintptr(unsafe.Pointer(&client.outputDesc)),
		uintptr(unsafe.Pointer(&client.attributes)), uintptr(unsafe.Pointer(&client.expiry)))
	runtime.KeepAlive(token)
	runtime.KeepAlive(spn)
	code := uint32(status)
	if client.output.data != nil {
		defer procFreeContextBuffer.Call(uintptr(client.output.data))
	}
	if code != sspiStatusOK && code != sspiStatusContinue {
		return nil, false, sspiError("initialize security context", code)
	}
	client.hasCtx = true
	return copyOwnedBytes(client.output.data, client.output.length), code == sspiStatusContinue, nil
}

func (client *kerberosClient) GetMIC(message []byte) ([]byte, error) {
	if err := client.loadSizes(); err != nil {
		return nil, err
	}
	signature := make([]byte, client.sizes.maxSignature)
	client.message = [2]sspiBuffer{
		{length: uint32(len(message)), kind: sspiBufferData},
		{length: uint32(len(signature)), kind: sspiBufferToken, data: unsafe.Pointer(&signature[0])},
	}
	if len(message) != 0 {
		client.message[0].data = unsafe.Pointer(&message[0])
	}
	client.messageDesc = sspiBufferDesc{version: 0, count: uint32(len(client.message)), buffers: unsafe.Pointer(&client.message[0])}
	status, _, _ := procMakeSignature.Call(uintptr(unsafe.Pointer(&client.ctx)), 0, uintptr(unsafe.Pointer(&client.messageDesc)), 0)
	runtime.KeepAlive(message)
	runtime.KeepAlive(signature)
	if code := uint32(status); code != sspiStatusOK {
		return nil, sspiError("sign SSH authentication", code)
	}
	return signature[:client.message[1].length], nil
}

func (client *kerberosClient) DeleteSecContext() error {
	if client.hasCtx {
		client.hasCtx = false
		procDeleteSecurityContext.Call(uintptr(unsafe.Pointer(&client.ctx)))
	}
	if client.hasCred {
		client.hasCred = false
		procFreeCredentialsHandle.Call(uintptr(unsafe.Pointer(&client.cred)))
	}
	return nil
}

// acquire takes the logged-on user's Kerberos credentials. The package is named
// explicitly, so SSPI never falls back to NTLM.
func (client *kerberosClient) acquire() error {
	pkg, err := syscall.UTF16PtrFromString("Kerberos")
	if err != nil {
		return err
	}
	status, _, _ := procAcquireCredentialsW.Call(
		0, uintptr(unsafe.Pointer(pkg)), sspiCredOutbound, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&client.cred)), uintptr(unsafe.Pointer(&client.expiry)))
	runtime.KeepAlive(pkg)
	if code := uint32(status); code != sspiStatusOK {
		return sspiError("acquire Kerberos credentials", code)
	}
	client.hasCred = true
	return nil
}

// loadSizes reads the signature size once; MakeSignature needs a buffer that
// large.
func (client *kerberosClient) loadSizes() error {
	client.sizeOnce.Do(func() {
		status, _, _ := procQueryContextAttributesW.Call(
			uintptr(unsafe.Pointer(&client.ctx)), sspiAttributeSizes, uintptr(unsafe.Pointer(&client.sizes)))
		if code := uint32(status); code != sspiStatusOK {
			client.sizeErr = sspiError("read signature size", code)
		}
	})
	return client.sizeErr
}

// sspiServiceName converts the GSS-API name the SSH layer passes, host@name, to
// the SSPI service principal host/name.
func sspiServiceName(target string) string {
	if rest, ok := strings.CutPrefix(target, "host@"); ok {
		return "host/" + rest
	}
	return target
}

// copyOwnedBytes copies a buffer SSPI allocated, so the caller can free it.
func copyOwnedBytes(data unsafe.Pointer, length uint32) []byte {
	if data == nil || length == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice((*byte)(data), int(length))...)
}

// sspiError names the failed call and the system's text for the status.
func sspiError(operation string, status uint32) error {
	var text [512]uint16
	n, _ := syscall.FormatMessage(sspiFormatFromSystem|sspiFormatIgnoreInserts, 0, status, 0, text[:], nil)
	message := strings.TrimSpace(syscall.UTF16ToString(text[:n]))
	if message == "" {
		return fmt.Errorf("Kerberos %s: SSPI status %#x", operation, status)
	}
	return fmt.Errorf("Kerberos %s: %s (%#x)", operation, message, status)
}

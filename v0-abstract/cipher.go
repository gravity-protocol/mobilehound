package abstract

import (
	"errors"

	"mobilehound/v0-subtle"
	"mobilehound/v0-util"
)

// CipherState defines an interface to an abstract symmetric message cipher.
// The cipher embodies a scalar that may be used to encrypt/decrypt data
// as well as to generate cryptographically random bits.
// The Cipher can also cryptographically absorb data or key material,
// updating its state to produce cryptographic hashes and authenticators.
//
// The main Message method processes a complete message through the Cipher,
// XORing a src byte-slice with cryptographic random bits to yield dst bytes,
// and concurrently absorbing bytes from a key byte-slice into its state:
//
//     cipher.Message(dst, src, key) Cipher
//
// A call always processes exactly max(len(dst),len(dst),len(key)) bytes.
// All slice arguments may be nil or of varying lengths.
// If the src or key slices are short, the missing bytes are taken to be zero.
// If the dst slice is short, the extra output bytes are discarded.
// The src and/or key slices may overlap with dst exactly or not at all.
//
// The cipher preserves and cryptographically accounts for message boundaries,
// so that the following sequence of two calls yields a result
// that is always cryptographically distinct from the above single call.
//
//     cipher.Message(dst[:div], src[:div], key[:div])
//     cipher.Message(dst[div:], src[div:], key[div:])
//
// The cipher guarantees that any key material absorbed during a given call
// will cryptographically affect every bit of all future messages processed,
// but makes no guarantees about whether key material absorbed in this call
// will affect some, all, or none of the cryptographic pseudorandom bits
// produced concurrently in the same call.
//
// A message cipher supports "full-duplex" operation,
// concurrently producing pseudorandom bits and absorbing data,
// supporting efficient use for authenticated encryption.
// This sequence of calls encrypts a plaintext msg to produce a ciphertext ctx
// and an associated message-authenticator mac:
//
//	cipher.Message(ctx, msg, ctx)	// Encrypt and absorb ciphertext
//	cipher.Message(mac, nil, nil)	// Produce MAC based on ciphertext
//
// This encrypts msg into ctx by XORing it with bits generated by the cipher,
// while absorbing the output ciphertext into the cipher's state.
// The second Message call then uses the resulting state to produce a MAC.
//
// The following sequence decrypts and verifies a received ciphertext and MAC
// encrypted in the above fashion:
//
//	cipher.Message(msg, ctx, ctx)	// Decrypt and absorb ciphertext
//	cipher.Message(mac, mac, nil)	// Compute MAC and XOR with received
//	valid := v0-subtle.ConstantTimeAllEq(mac, 0)
//
// This decrypts ctx into msg by XORing the same bits used during encryption,
// while similarly absorbing the ciphertext (which is the input this time).
// The second Message call recomputes the MAC based on the absorbed ciphertext,
// XORs the recomputed MAC onto the received MAC in-place,
// and verifies in constant time that the result is zero
// (i.e., that the received and recomputed MACs are equal).
//
// The Cipher wrapper provides convenient Seal and Open functions
// performing the above authenticated encryption sequences.
//
// A cipher may be operated as a cryptographic hash function taking
// messsage msg and producing cryptographic checksum in slice sum:
//
//	cipher.Message(nil, nil, msg)	// Absorb msg into Cipher state
//	cipher.Message(sum, nil, nil)	// Produce cryptographic hash in sum
//
// Both the input msg and output sum may be of any length,
// and the Cipher guarantees that every bit of the output sum has a
// strong cryptographic dependency on every bit of the input msg.
// However, to achieve full security, the caller should ensure that
// the output sum is at least cipher.HashSize() bytes long.
//
// The Partial method processes a partial (initial or continuing) portion
// of a message, allowing the Cipher to be used for byte-granularity streaming:
//
//	cipher.Partial(dst, src, key)
//
// The above single call is thus equivalent to the following pair of calls:
//
//	cipher.Partial(dst[:div], src[:div], key[:div])
//	cipher.Partial(dst[div:], src[div:], key[div:])
//
// One or more calls to Partial must be terminated with a call to Message,
// to complete the message and ensure that key-material bytes absorbed
// in the current message affect the pseudorandom bits the Cipher produces
// in the context of the next message.
// Key material absorbed in a given Partial call may, or may not,
// affect the pseudorandom bits generated in subsequent Partial calls
// if there are no intervening calls to Message.
//
// A Cipher may be used to generate pseudorandom bits that depend
// only on the Cipher's initial state in the following fashion:
//
//	cipher.Partial(dst, nil, nil)
//
type CipherState interface {

	// Transform a message (or the final portion of one) from src to dst,
	// absorb key into the cipher state, and return the Cipher.
	Message(dst, src, key []byte)

	// Transform a partial, incomplete message from src to dst,
	// absorb key into the cipher state, and return the Cipher.
	Partial(dst, src, key []byte)

	// Return the minimum size in bytes of secret keys for full security
	// (although key material may be of any size).
	KeySize() int

	// Return recommended size in bytes of hashes for full security.
	// This is usually 2*KeySize() to account for birthday attacks.
	HashSize() int

	// Create an identical clone of this cryptographic state object.
	// Caution: misuse can lead to key-reuse vulnerabilities.
	Clone() CipherState
}

// internal type for the simple options above
type option struct{ name string }

func (o *option) String() string { return o.name }

// Pass NoKey to a Cipher constructor to create an unkeyed Cipher.
var NoKey = []byte{}

// Pass RandomKey to a Cipher constructor to create a randomly seeded Cipher.
var RandomKey []byte = nil

// Cipher represents a general-purpose symmetric message cipher.
// A Cipher instance embodies a scalar that may be used to encrypt/decrypt data
// as well as to generate cryptographically random bits.
// The Cipher can also cryptographically absorb data or key material,
// updating its state to produce cryptographic hashes and authenticators.
// Using these encryption and absorption functions in combination,
// a Cipher may be used for authenticated encryption and decryption.
//
// A Cipher is in fact simply a convenience/helper wrapper around
// the CipherState interface, which represents and abstracts over
// an underlying message cipher implementation.
// The underlying CipherState instance typically embodies
// both the specific message cipher algorithm in use,
// and the choice of security parameter with which the cipher is operated.
// This algorithm and security parameter is typically set
// when the Cipher (and its underlying CipherState) instance is constructed.
// The simplest way to get a Cipher instance is via a cipher Suite.
//
// The standard function signature for a Cipher constructor is:
//
//	NewCipher(key []byte, options ...interface{}) Cipher
//
// If key is nil, the Cipher constructor picks a fresh, random key.
// The key may be an empty but non-nil slice to create an unkeyed cipher.
// Key material may be of any length, but to ensure full security,
// secret keys should be at least the size returned by the KeySize method.
// The variable-length options argument may contain options
// whose interpretation is specific to the particular cipher.
// (XXX may reconsider the wisdom of this options convention;
// its lack of type-checking has led to accidental confusion at least once.)
//
type Cipher struct {
	CipherState // underlying message cipher implementation
}

// Message processes a complete message through the Cipher,
// XORing a src byte-slice with cryptographic random bits to yield dst bytes,
// and concurrently absorbing bytes from a key byte-slice into its state:
//
//	cipher.Message(dst, src, key) Cipher
//
// A call always processes exactly max(len(dst),len(dst),len(key)) bytes.
// All slice arguments may be nil or of varying lengths.
// If the src or key slices are short, the missing bytes are taken to be zero.
// If the dst slice is short, the extra output bytes are discarded.
// The src and/or key slices may overlap with dst exactly or not at all.
//
// The Cipher preserves and cryptographically accounts for message boundaries,
// so that the following sequence of two calls yields a result
// that is always cryptographically distinct from the above single call.
//
//	cipher.Message(dst[:div], src[:div], key[:div])
//	cipher.Message(dst[div:], src[div:], key[div:])
//
// The Cipher guarantees that any key material absorbed during a given call
// will cryptographically affect every bit of all future messages processed,
// but makes no guarantees about whether key material absorbed in this call
// will affect some, all, or none of the cryptographic pseudorandom bits
// produced concurrently in the same call.
//
func (c Cipher) Message(dst, src, key []byte) Cipher {
	c.CipherState.Message(dst, src, key)
	return c
}

// Partial processes a partial (initial or continuing) portion of a message,
// allowing the Cipher to be used for byte-granularity streaming:
//
//	cipher.Partial(dst, src, key)
//
// The above single call is thus equivalent to the following pair of calls:
//
//	cipher.Partial(dst[:div], src[:div], key[:div])
//	cipher.Partial(dst[div:], src[div:], key[div:])
//
// One or more calls to Partial must be terminated with a call to Message,
// to complete the message and ensure that key-material bytes absorbed
// in the current message affect the pseudorandom bits the Cipher produces
// in the context of the next message.
// Key material absorbed in a given Partial call may, or may not,
// affect the pseudorandom bits generated in subsequent Partial calls
// if there are no intervening calls to Message.
//
func (c Cipher) Partial(dst, src, key []byte) Cipher {
	c.CipherState.Partial(dst, src, key)
	return c
}

// Read satisfies the standard io.Reader interface,
// yielding a stream of cryptographically pseudorandom bytes.
// Consistent with the streaming semantics of the io.Reader interface,
// two consecutive reads of length l1 and l2 produce the same bytes
// as a single read of length l1+l2.
//
func (c Cipher) Read(dst []byte) (n int, err error) {
	c.CipherState.Partial(dst, nil, nil)
	return len(dst), nil
}

// Write satisifies the standard io.Writer interface,
// cryptographically absorbing all written data into the Cipher's state.
// Consistent with the streaming semantics of the io.Writer interface,
// Write calls by themselves never produce message boundaries,
// and written data is NOT guaranteed to affect the Cipher's output
// until the next explicit message boundary.
// The caller should invoke EndMessage after a series of Write calls
// to ensure that all written data is fully absorbed into the Cipher,
// before reading Cipher output that is supposed to depend on the written data.
//
func (c Cipher) Write(key []byte) (n int, err error) {
	c.CipherState.Partial(nil, nil, key)
	return len(key), nil
}

// EndMessage inserts an explicit end-of-message boundary,
// finalizing the message currently being processed and starting a new one.
// The client should typically call EndMessage after a series of
// calls to streaming methods such as Partial, Read, or Write.
//
func (c Cipher) EndMessage() {
	c.CipherState.Message(nil, nil, nil) // finalize the current message
}

// XORKeyStream satisfies the Go library's legacy cipher.Stream interface,
// enabling a Cipher to be used as a stream cipher.
// This method reads len(src) pseudorandom bytes from the Cipher,
// XORs them with the corresponding bytes from src,
// and writes the resulting stream-encrypted bytes to dst.
// The dst slice must be at least as long as src,
// and if it is longer, only the corresponding prefix of dst is affected.
//
// Warning: stream ciphers inherently provide no authentication,
// and malicious bit-flipping attacks are trivial if the encrypted stream
// is not authenticated in some other way.
// For this reason, stream cipher operation is not recommended
// in common-case situations in which authenticated encryption methods
// (e.g., via Seal and Open) are applicable.
//
func (c Cipher) XORKeyStream(dst, src []byte) {
	c.CipherState.Partial(dst[:len(src)], src, nil)
}

// Sum ends the current message and produces a cryptographic checksum or hash
// based on the Cipher's state after absorbing all previously-written data.
// The resulting hash is appended to the dst slice,
// which Sum will grow or allocate if dst is too small or nil.
// A Cipher may be used as a hash function by absorbing data via Write
// and then calling Sum to finalize the message and produce the hash.
// Unlike the hash.Hash interface, this Sum method affects the Cipher's state:
// two consecutive calls to Sum on the same Cipher
// will produce two different hashes, not the same one.
//
func (c Cipher) Sum(dst []byte) []byte {
	c.EndMessage() // finalize any message in progress

	h := c.HashSize() // hash length
	dst, hash := util.Grow(dst, h)
	c.Message(hash, nil, nil) // squeeze out hash

	return dst
}

// Seal uses a stateful message cipher to implement authenticated encryption.
// It encrypts the src message and appends it to the dst slice,
// growing or allocating the dst slice if it is too small or nil.
// Seal also absorbs the produced ciphertext into the Cipher's state,
// then uses that state to append a message authentication check (MAC)
// to the sealed message, to be verified by Open.
//
func (c Cipher) Seal(dst, src []byte) []byte {
	l := len(src)    // message length
	m := c.KeySize() // MAC length

	dst, buf := util.Grow(dst, l+m)
	ctx := buf[:l]
	mac := buf[l:]

	c.Message(ctx, src, ctx) // Encrypt and absorb ciphertext
	c.Message(mac, nil, nil) // Append MAC

	return dst
}

// Open decrypts and authenticates a message encrypted using Seal.
// It decrypts sealed message src and appends it onto plaintext buffer dst,
// growing the dst buffer if it is too small (or nil),
// and returns the resulting destination buffer or an error.
//
func (c Cipher) Open(dst, src []byte) ([]byte, error) {
	m := c.KeySize()
	l := len(src) - m
	if l < 0 {
		return nil, errors.New("sealed ciphertext too short")
	}
	ctx := src[:l]
	mac := src[l:]
	dst, msg := util.Grow(dst, l)

	if &msg[0] != &ctx[0] { // Decrypt and absorb ciphertext
		c.Message(msg, ctx, ctx)
	} else {
		tmp := make([]byte, l)
		c.Message(tmp, ctx, ctx)
		copy(msg, tmp)
	}

	c.Message(mac, mac, nil) // Compute MAC and XOR with received
	if v0-subtle.ConstantTimeAllEq(mac, 0) == 0 {
		return nil, errors.New("ciphertext authentication failed")
	}

	return dst, nil
}

// XXX Fork off nsubs >= 0 parallel sub-Ciphers and update the state.
//	Fork(nsubs int) []Cipher

// XXX Combine this Cipher's state with that of previously-forked Ciphers.
// The rejoined sub-Ciphers must no longer be used.
//	Join(subs ...Cipher)

// Clone creates an initially identicial instance of a Cipher.
// Warning:: misuse of Clone can lead to replay or key-reuse vulnerabilities.
func (c Cipher) Clone() Cipher {
	return Cipher{c.CipherState.Clone()}
}

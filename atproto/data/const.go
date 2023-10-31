package data

const (
	// maximum size of any CBOR data, in any context, in atproto
	MAX_CBOR_SIZE = 5 * 1024 * 1024
	// maximum serialized size of an individual atproto record, in CBOR format
	MAX_CBOR_RECORD_SIZE = 1 * 1024 * 1024
	// maximum serialized size of an individual atproto record, in JSON format
	MAX_JSON_RECORD_SIZE = 2 * 1024 * 1024
	// maximum serialized size of blocks (raw bytes) in an atproto repo stream event (NOT ENFORCED YET)
	MAX_STREAM_REPO_DIFF_SIZE = 4 * 1024 * 1024
	// maximum size of a WebSocket frame in atproto event streams (NOT ENFORCED YET)
	MAX_STREAM_FRAME_SIZE = MAX_CBOR_SIZE
	// maximum size of any individual string inside an atproto record
	MAX_RECORD_STRING_LEN = MAX_CBOR_RECORD_SIZE
	// maximum size of any individual byte array (bytestring) inside an atproto record
	MAX_RECORD_BYTES_LEN = MAX_CBOR_RECORD_SIZE
	// limit on size of CID representation (NOT ENFORCED YET)
	MAX_CID_BYTES = 100
	// limit on depth of nested containers (objects or arrays) for atproto data (NOT ENFORCED YET)
	MAX_CBOR_NESTED_LEVELS = 32
	// maximum number of elements in an object or array in atproto data
	MAX_CBOR_CONTAINER_LEN = 128 * 1024
	// largest integer which can be represented in a float64. integers in atproto "should" not be larger than this. (NOT ENFORCED)
	MAX_SAFE_INTEGER = 9007199254740991
	// largest negative integer which can be represented in a float64. integers in atproto "should" not go below this. (NOT ENFORCED)
	MIN_SAFE_INTEGER = -9007199254740991
	// maximum length of string (UTF-8 bytes) in an atproto object (map)
	MAX_OBJECT_KEY_LEN = 8192
)

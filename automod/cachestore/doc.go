// Automod component for caching arbitrary data (as JSON strings) with a fixed TTL and purging.
//
// Includes an interface and implementations using redis and in-process memory.
//
// This is used by the rules engine to cache things like account metadata, improving latency and reducing load on authoritative backend systems.
package cachestore

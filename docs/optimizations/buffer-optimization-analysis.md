# Buffer Optimization Analysis

**Date:** 2026-04-23
**Last Updated:** 2026-04-24
**Status:** Closed — No changes needed
**Author:** Claude

---

## Background

This document analyzes the streaming buffer implementation in the proxy handler and proposes optimizations that maintain the transparent proxy design principles.

## Current Implementation

In `internal/proxy/handler.go`:

```go
buf := make([]byte, 32*1024)
for {
    n, err := resp.Body.Read(buf)
    if n > 0 {
        w.Write(buf[:n])
        if canFlush {
            flusher.Flush()
        }
    }
    if err != nil {
        break
    }
}
```

## Key Findings

### 1. SSE Streaming Requires Immediate Delivery

SSE (Server-Sent Events) requires events to be delivered to the client as they arrive. This is why we can't simply use `io.Copy()` which buffers internally and only flushes when buffer is full.

The explicit buffer + flush pattern ensures progressive delivery:
- Each read from upstream is immediately written and flushed
- Client receives events in real-time, not buffered

### 2. Buffer Size Analysis

**Anthropic does NOT officially document per-event size limits.** Research against official docs and community reports confirms there is no published maximum for individual `content_block_delta` events.

**Typical Anthropic SSE event sizes (observed, not guaranteed):**

| Event Type | Approximate Size |
|------------|------------------|
| content_block_delta (text token) | 100-500 bytes |
| Tool result | 200-1000 bytes |
| Large code block delta | up to 4KB+ |
| Compaction summary (single delta) | potentially much larger |
| Tool use input_json_delta (large JSON) | potentially much larger |

**Key edge cases that can produce large single deltas:**
- **Compaction summaries** — sent as a single `content_block_delta` with no intermediate streaming, can be conversation-summary-sized
- **Tool use `input_json_delta`** — can contain large JSON payloads
- **Extended thinking blocks** — thinking deltas can accumulate significantly

**32KB vs alternatives:**

| Buffer Size | Pros | Cons |
|-------------|------|------|
| 4KB | Less memory per request | Multiple reads for large events |
| 8KB | Handles most typical events in one read | Risk of multiple reads for compaction/tool-use deltas |
| 16KB | Higher safety margin for edge cases | Still less headroom than 32KB |
| 32KB | Fewer reads, handles all known edge cases | Higher per-request memory (32KB) |

**Conclusion:** 32KB is the safest choice. No official documentation confirms 8KB is always sufficient. The per-request memory cost (32KB) is negligible for the project's scale (5-20 users). The existing code already handles events larger than the buffer correctly via multiple loop iterations — the buffer size only affects syscall count, not correctness.

### 3. io.Copy Limitation

`io.Copy(w, resp.Body)` cannot be used because:
- It buffers internally (8KB default)
- Does not flush after each event
- SSE clients expect immediate delivery
- Would cause visible lag/delayed streaming

### 4. Go's Internal Memory Allocator (tcmalloc-based)

Go's memory allocator has **size-classed per-P (per-processor) caches**. When a `make([]byte, 32*1024)` buffer is GC'd, Go does NOT immediately return that memory to the OS. It keeps it in the per-P cache. The next allocation of the same size class on that same P reuses the memory almost instantly — no syscall, no heap traversal.

This means Go's runtime is already effectively pooling streaming buffers without any explicit `sync.Pool`.

### 5. Why `sync.Pool` Would Be Harmful Here

`sync.Pool` is often suggested for buffer reuse, but for this project it would be counterproductive:

1. **Go clears all `sync.Pool` entries on every GC cycle.** The pool only helps if buffers are reused *within a single GC cycle*. Under moderate load where GC runs frequently, the pool gets flushed before reuse — making it roughly equivalent to Go's own per-P cache, but with extra overhead.

2. **Atomic overhead on every Get/Put.** `sync.Pool` uses atomic operations internally. Each `Get()` and `Put()` involves a CAS. Go's per-P cache is faster because it's P-local — no atomics needed.

3. **Bug surface.** Buffers must be reset (slice length, not capacity) between uses. Forgetting this leaks data between requests — a correctness risk for a transparent proxy.

4. **`bufio.Reader` wrapping is redundant.** Go's `net/http` transport already uses internal `bufio.Reader` (4KB). Wrapping `resp.Body` with another `bufio.NewReaderSize` creates double-buffering with no benefit.

## Proposed Optimization (Rejected)

~~Replace explicit 32KB buffer with `bufio.Reader` using 8KB internal buffer.~~

**This proposal was rejected after further analysis:**
- 8KB is not verified as sufficient for all Anthropic SSE events (no official docs on per-event size limits)
- `bufio.Reader` wrapping is redundant — Go's `net/http` transport already buffers internally
- `sync.Pool` adds complexity and atomic overhead for negligible gain given Go's per-P allocator caching
- The 32KB per-request memory cost is negligible at the project's scale

## Final Recommendation

**No changes needed.** The current implementation (32KB buffer, no pool, no bufio.Reader wrapping) is already well-optimized:

- 32KB handles all known SSE event sizes including compaction and tool-use edge cases
- Go's per-P allocator cache effectively reuses buffer memory across requests
- The explicit buffer + flush pattern correctly preserves SSE streaming semantics
- The implementation is simple, correct, and has no extra moving parts

---

## Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-04-23 | Proposed 8KB buffer + bufio.Reader | Initial analysis assumed typical events are <8KB |
| 2026-04-24 | Rejected — keep 32KB, no changes | No official docs confirm 8KB always sufficient; Go's allocator already pools effectively; simplicity > micro-optimization for a reliability-focused project |

---

## References

- [Anthropic Streaming Documentation](https://docs.anthropic.com/en/docs/build-with-claude/streaming)
- [Anthropic Compaction](https://platform.claude.com/docs/en/build-with-claude/compaction)
- [Anthropic Extended Thinking](https://platform.claude.com/docs/en/build-with-claude/extended-thinking)
- [GitHub issue: connection closes without message_stop event](https://github.com/anthropics/anthropic-sdk-typescript/issues/842)

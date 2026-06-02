// Context-carriage Go reference (PU-2). Standard library only — no SDK dependency, so it
// runs offline with no module downloads. The suite conforms the stamping LOGIC, not the
// proto wire-bytes (the shared connectionless codegen owns those — ADR-018).
module rat.dev/conformance/context-carriage

go 1.25

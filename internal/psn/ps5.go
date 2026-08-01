package psn

// ps5FirmwareToken is the fixed obfuscation token Sony embeds in the PS5
// firmware update URL path.  It does not change per-locale or per-version —
// Sony bakes it into every client that needs to fetch PS5 firmware.
// Extracted verbatim from PySN.py.
const ps5FirmwareToken = "tJMRE80IbXnE9YuG0jzTXgKEjIMoabr6"

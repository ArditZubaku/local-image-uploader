package storage

// Byte-exact size accounting for the archives ZipPlan.Stream produces.
//
// Knowing the total up front is what lets the zip route send a real
// Content-Length, so a phone shows a progress bar and an ETA instead of an
// open-ended spinner. The constants below mirror archive/zip's writer for
// the one shape we emit: Store, streamed, with a data descriptor and an
// Info-ZIP extended-timestamp extra. zipsize_test.go asserts the formula
// against a real zip.Writer so a Go upgrade can't drift it unnoticed.
const (
	localHeaderLen     = 30
	centralHeaderLen   = 46
	endOfCentralDirLen = 22
	dataDescriptorLen  = 16
	dataDescriptor64   = 24
	zip64ExtraLen      = 28
	zip64EndLen        = 56 + 20
	extTimestampLen    = 9

	uint32Max = 1<<32 - 1
	uint16Max = 1<<16 - 1
)

// zipSize returns the exact byte length of the archive ZipPlan.Stream would
// write for these entries, in this order.
func zipSize(entries []ZipEntry) int64 {
	var total int64

	offsets := make([]int64, len(entries))
	for i, e := range entries {
		offsets[i] = total
		total += localHeaderLen + int64(len(e.Name)) + extTimestampLen
		total += e.Size
		if e.Size >= uint32Max {
			total += dataDescriptor64
		} else {
			total += dataDescriptorLen
		}
	}

	centralStart := total
	for i, e := range entries {
		total += centralHeaderLen + int64(len(e.Name)) + extTimestampLen
		if e.Size >= uint32Max || offsets[i] >= uint32Max {
			total += zip64ExtraLen
		}
	}
	centralSize := total - centralStart

	total += endOfCentralDirLen
	if len(entries) >= uint16Max || centralSize >= uint32Max || centralStart >= uint32Max {
		total += zip64EndLen
	}

	return total
}

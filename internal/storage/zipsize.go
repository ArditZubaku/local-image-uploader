package storage

// Byte-exact size accounting for the archives ZipPlan.Stream produces.
//
// Knowing the total up front is what lets the zip route send a real
// Content-Length, so a phone shows a progress bar and an ETA instead of an
// open-ended spinner. The constants and conditions below mirror
// archive/zip's writer for the one shape we emit: Store, streamed, with a
// data descriptor and an Info-ZIP extended-timestamp extra.
//
// Getting this even slightly wrong is worse than not sending a length at
// all: an archive one byte shorter than promised leaves the phone waiting
// forever for a byte that never comes. zipsize_test.go asserts the formula
// against a real zip.Writer across every branch below, including the zip64
// boundaries, so a Go upgrade can't drift it unnoticed.
const (
	localHeaderLen     = 30
	centralHeaderLen   = 46
	endOfCentralDirLen = 22
	dataDescriptorLen  = 16
	dataDescriptor64   = 24
	zip64EndLen        = 56 + 20
	extTimestampLen    = 9

	// A zip64 extra in the central directory is a 4-byte header followed
	// by only the fields that actually overflow, not a fixed block.
	zip64ExtraHeader = 4
	zip64SizeFields  = 16 // compressed and uncompressed, 8 bytes each
	zip64OffsetField = 8

	uint32Max = 1<<32 - 1
	uint16Max = 1<<16 - 1
)

// zipSize returns the exact byte length of the archive ZipPlan.Stream
// would write for these entries, in this order.
func zipSize(entries []ZipEntry) int64 {
	var total int64

	offsets := make([]int64, len(entries))
	for i, e := range entries {
		offsets[i] = total
		total += localHeaderLen + int64(len(e.Name)) + extTimestampLen
		total += e.Size

		// Stored entries compress to their own size, so one test covers
		// both size fields. Note the strict ">": the data descriptor and
		// the central directory disagree on the boundary, and an entry of
		// exactly 4 GiB - 1 takes the 32-bit descriptor but still gets a
		// zip64 extra below.
		if e.Size > uint32Max {
			total += dataDescriptor64
		} else {
			total += dataDescriptorLen
		}
	}

	usedZip64 := false
	centralStart := total
	for i, e := range entries {
		total += centralHeaderLen + int64(len(e.Name)) + extTimestampLen

		bigSize := e.Size >= uint32Max
		bigOffset := offsets[i] >= uint32Max
		if !bigSize && !bigOffset {
			continue
		}

		usedZip64 = true
		total += zip64ExtraHeader
		if bigSize {
			total += zip64SizeFields
		}
		if bigOffset {
			total += zip64OffsetField
		}
	}
	centralSize := total - centralStart

	total += endOfCentralDirLen
	// archive/zip emits the zip64 end records whenever any entry needed a
	// zip64 extra, even when the directory's own fields would still fit.
	if usedZip64 || len(entries) >= uint16Max || centralSize >= uint32Max || centralStart >= uint32Max {
		total += zip64EndLen
	}

	return total
}

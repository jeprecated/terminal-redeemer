package sliceprotocol

import "sort"

func SupportedVersions() []uint32 { return []uint32{SchemaVersion} }

func Negotiate(accepted []uint32) (uint32, bool) {
	if len(accepted) == 0 {
		accepted = SupportedVersions()
	}
	supported := make(map[uint32]struct{})
	for _, version := range SupportedVersions() {
		supported[version] = struct{}{}
	}
	values := append([]uint32(nil), accepted...)
	sort.Slice(values, func(i, j int) bool { return values[i] > values[j] })
	for _, version := range values {
		if _, ok := supported[version]; ok {
			return version, true
		}
	}
	return 0, false
}

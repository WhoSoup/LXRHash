// Copyright (c) of parts are held by the various contributors
// Licensed under the MIT License. See LICENSE file in the project root for full license information.
package lxr

// LXRHash holds one instance of a hash function with a specific seed and map size
type LXRHash struct {
	ByteMap     []byte // Integer Offsets
	MapSize     uint64 // Size of the translation table
	MapSizeBits uint64 // Size of the ByteMap in Bits
	Passes      uint64 // Passes to generate the rand table
	Seed        uint64 // An arbitrary number used to create the tables.
	HashSize    uint64 // Number of bytes in the hash
	verbose     bool
}

func (lx LXRHash) HashWork(baseData []byte, batch [][]byte) [][]byte {
	fullL := len(baseData) + len(batch[0])
	hss := make([][]uint64, len(batch))
	ass := make([]uint64, len(batch))
	s1s := make([]uint64, len(batch))
	s2s := make([]uint64, len(batch))
	s3s := make([]uint64, len(batch))
	idxs := make([]uint64, len(batch))
	mk := lx.MapSize - 1

	for i := 0; i < len(batch); i++ {
		hss[i] = make([]uint64, lx.HashSize)
		ass[i] = lx.Seed
	}

	base := func(b, idx int) byte {
		if idx >= len(baseData) {
			return batch[b][idx-len(baseData)]
		}
		return baseData[idx]
	}

	// Fast spin to prevent caching state
	for x := 0; x < fullL; x++ {
		for i := 0; i < len(batch); i++ {
			if idxs[i] >= lx.HashSize { // Use an if to avoid modulo math
				idxs[i] = 0
			}

			ass[i], s1s[i], s2s[i], s3s[i] = lx.fastStepf(uint64(base(i, x)), ass[i], s1s[i], s2s[i], s3s[i], idxs[i], hss[i])
			idxs[i]++
		}
	}

	idxs = make([]uint64, len(batch))
	// Actual work to compute the hash
	for x := 0; x < fullL; x++ {
		for i := 0; i < len(batch); i++ {
			if idxs[i] >= lx.HashSize { // Use an if to avoid modulo math
				idxs[i] = 0
			}
			v2 := uint64(base(i, x))
			ass[i], s1s[i], s2s[i], s3s[i] = lx.stepf(ass[i], s1s[i], s2s[i], s3s[i], v2, hss[i], idxs[i], mk)
			idxs[i]++
		}
	}

	// Reduction pass
	// Done by Interating over hs[] to produce the bytes[] hash
	//
	// At this point, we have HBits of state in hs.  We need to reduce them down to a byte,
	// And we do so by doing a bit more bitwise math, and mapping the values through our byte map.

	bytes := make([][]byte, len(batch))
	for i := range bytes {
		bytes[i] = make([]byte, lx.HashSize)
	}
	// Roll over all the hs (one int64 value for every byte in the resulting hash) and reduce them to byte values
	for j := int(lx.HashSize) - 1; j >= 0; j-- {
		for i := 0; i < len(batch); i++ {
			ass[i], s1s[i], s2s[i], s3s[i] = lx.stepf(ass[i], s1s[i], s2s[i], s3s[i], uint64(hss[i][j]), hss[i], uint64(j), mk)
			bytes[i][j] = lx.ByteMap[ass[i]&mk] ^ lx.ByteMap[uint64(hss[i][j])&mk] // Xor two resulting sequences
		}
	}

	return bytes
}

func (lx LXRHash) fastStepf(v2, as, s1, s2, s3, idx uint64, hs []uint64) (uint64, uint64, uint64, uint64) {
	b := uint64(lx.ByteMap[(as^v2)&(lx.MapSize-1)])
	as = as<<7 ^ as>>5 ^ v2<<20 ^ v2<<16 ^ v2 ^ b<<20 ^ b<<12 ^ b<<4
	s1 = s1<<9 ^ s1>>3 ^ hs[idx]
	hs[idx] = s1 ^ as
	s1, s2, s3 = s3, s1, s2
	return as, s1, s2, s3
}

func (lx LXRHash) stepf(as, s1, s2, s3, v2 uint64, hs []uint64, idx uint64, mk uint64) (uint64, uint64, uint64, uint64) {
	s1 = s1<<9 ^ s1>>1 ^ as ^ uint64(lx.ByteMap[(as>>5^v2)&mk])<<3      // Shifts are not random.  They are selected to ensure that
	s1 = s1<<5 ^ s1>>3 ^ uint64(lx.ByteMap[(s1^v2)&mk])<<7              // Prior bytes pulled from the ByteMap contribute to the
	s1 = s1<<7 ^ s1>>7 ^ uint64(lx.ByteMap[(as^s1>>7)&mk])<<5           // next access of the ByteMap, either by contributing to
	s1 = s1<<11 ^ s1>>5 ^ uint64(lx.ByteMap[(v2^as>>11^s1)&mk])<<27     // the lower bits of the index, or in the upper bits that
	_ = 0                                                               // move the access further in the map.
	hs[idx] = s1 ^ as ^ hs[idx]<<7 ^ hs[idx]>>13                        //
	_ = 0                                                               // We also pay attention not only to where the ByteMap bits
	as = as<<17 ^ as>>5 ^ s1 ^ uint64(lx.ByteMap[(as^s1>>27^v2)&mk])<<3 // are applied, but what bits we use in the indexing of
	as = as<<13 ^ as>>3 ^ uint64(lx.ByteMap[(as^s1)&mk])<<7             // the ByteMap
	as = as<<15 ^ as>>7 ^ uint64(lx.ByteMap[(as>>7^s1)&mk])<<11         //
	as = as<<9 ^ as>>11 ^ uint64(lx.ByteMap[(v2^as^s1)&mk])<<3          // Tests run against this set of shifts show that the
	_ = 0                                                               // bytes pulled from the ByteMap are evenly distributed
	s1 = s1<<7 ^ s1>>27 ^ as ^ uint64(lx.ByteMap[(as>>3)&mk])<<13       // over possible byte values (0-255) and indexes into
	s1 = s1<<3 ^ s1>>13 ^ uint64(lx.ByteMap[(s1^v2)&mk])<<11            // the ByteMap are also evenly distributed, and the
	s1 = s1<<8 ^ s1>>11 ^ uint64(lx.ByteMap[(as^s1>>11)&mk])<<9         // deltas between bytes provided map to a curve expected
	s1 = s1<<6 ^ s1>>9 ^ uint64(lx.ByteMap[(v2^as^s1)&mk])<<3           // (fewer maximum and minimum deltas, and most deltas around
	_ = 0                                                               // zero.
	as = as<<23 ^ as>>3 ^ s1 ^ uint64(lx.ByteMap[(as^v2^s1>>3)&mk])<<7
	as = as<<17 ^ as>>7 ^ uint64(lx.ByteMap[(as^s1>>3)&mk])<<5
	as = as<<13 ^ as>>5 ^ uint64(lx.ByteMap[(as>>5^s1)&mk])<<1
	as = as<<11 ^ as>>1 ^ uint64(lx.ByteMap[(v2^as^s1)&mk])<<7
	s1 = s1<<5 ^ s1>>3 ^ as ^ uint64(lx.ByteMap[(as>>7^s1>>3)&mk])<<6
	s1 = s1<<8 ^ s1>>6 ^ uint64(lx.ByteMap[(s1^v2)&mk])<<11
	s1 = s1<<11 ^ s1>>11 ^ uint64(lx.ByteMap[(as^s1>>11)&mk])<<5
	s1 = s1<<7 ^ s1>>5 ^ uint64(lx.ByteMap[(v2^as>>7^as^s1)&mk])<<17
	s2 = s2<<3 ^ s2>>17 ^ s1 ^ uint64(lx.ByteMap[(as^s2>>5^v2)&mk])<<13
	s2 = s2<<6 ^ s2>>13 ^ uint64(lx.ByteMap[(s2)&mk])<<11
	s2 = s2<<11 ^ s2>>11 ^ uint64(lx.ByteMap[(as^s1^s2>>11)&mk])<<23
	s2 = s2<<4 ^ s2>>23 ^ uint64(lx.ByteMap[(v2^as>>8^as^s2>>10)&mk])<<1
	s1 = s2<<3 ^ s2>>1 ^ hs[idx] ^ v2
	as = as<<9 ^ as>>7 ^ s1>>1 ^ uint64(lx.ByteMap[(s2>>1^hs[idx])&mk])<<5

	s1, s2, s3 = s3, s1, s2

	return as, s1, s2, s3
}

// FlatHash takes the arbitrary input and returns the resulting hash of length HashSize
// Does not use anonymous functions
func (lx LXRHash) FlatHash(src []byte) []byte {
	// Keep the byte intermediate results as int64 values until reduced.
	hs := make([]uint64, lx.HashSize)
	// as accumulates the state as we walk through applying the source data through the lookup map
	// and combine it with the state we are building up.
	var as = lx.Seed
	// We keep a series of states, and roll them along through each byte of source processed.
	var s1, s2, s3 uint64
	// Since MapSize is specified in bits, the index mask is the size-1
	mk := lx.MapSize - 1

	idx := uint64(0)
	// Fast spin to prevent caching state
	for _, v2 := range src {
		if idx >= lx.HashSize { // Use an if to avoid modulo math
			idx = 0
		}

		as, s1, s2, s3 = lx.fastStepf(uint64(v2), as, s1, s2, s3, idx, hs)
		idx++
	}

	idx = 0
	// Actual work to compute the hash
	for _, v2 := range src {
		if idx >= lx.HashSize { // Use an if to avoid modulo math
			idx = 0
		}

		as, s1, s2, s3 = lx.stepf(as, s1, s2, s3, uint64(v2), hs, idx, mk)
		idx++
	}

	// Reduction pass
	// Done by Interating over hs[] to produce the bytes[] hash
	//
	// At this point, we have HBits of state in hs.  We need to reduce them down to a byte,
	// And we do so by doing a bit more bitwise math, and mapping the values through our byte map.

	bytes := make([]byte, lx.HashSize)
	// Roll over all the hs (one int64 value for every byte in the resulting hash) and reduce them to byte values
	for i := len(hs) - 1; i >= 0; i-- {
		as, s1, s2, s3 = lx.stepf(as, s1, s2, s3, uint64(hs[i]), hs, uint64(i), mk)
		bytes[i] = lx.ByteMap[as&mk] ^ lx.ByteMap[hs[i]&mk] // Xor two resulting sequences
	}

	// Return the resulting hash
	return bytes
}

// Hash takes the arbitrary input and returns the resulting hash of length HashSize
func (lx LXRHash) HashWithAnon(src []byte) []byte {
	// Keep the byte intermediate results as int64 values until reduced.
	hs := make([]uint64, lx.HashSize)
	// as accumulates the state as we walk through applying the source data through the lookup map
	// and combine it with the state we are building up.
	var as = lx.Seed
	// We keep a series of states, and roll them along through each byte of source processed.
	var s1, s2, s3 uint64
	// Since MapSize is specified in bits, the index mask is the size-1
	mk := lx.MapSize - 1

	B := func(v uint64) uint64 { return uint64(lx.ByteMap[v&mk]) }
	b := func(v uint64) byte { return byte(B(v)) }

	faststep := func(v2 uint64, idx uint64) {
		b := B(as ^ v2)
		as = as<<7 ^ as>>5 ^ v2<<20 ^ v2<<16 ^ v2 ^ b<<20 ^ b<<12 ^ b<<4
		s1 = s1<<9 ^ s1>>3 ^ hs[idx]
		hs[idx] = s1 ^ as
		s1, s2, s3 = s3, s1, s2
	}

	// Define a function to move the state by one byte.  This is not intended to be fast
	// Requires the previous byte read to process the next byte read.  Forces serial evaluation
	// and removes the possibility of scheduling byte access.
	//
	// (Note that use of _ = 0 in lines below are to keep go fmt from messing with comments on the right of the page)
	step := func(v2 uint64, idx uint64) {
		s1 = s1<<9 ^ s1>>1 ^ as ^ B(as>>5^v2)<<3      // Shifts are not random.  They are selected to ensure that
		s1 = s1<<5 ^ s1>>3 ^ B(s1^v2)<<7              // Prior bytes pulled from the ByteMap contribute to the
		s1 = s1<<7 ^ s1>>7 ^ B(as^s1>>7)<<5           // next access of the ByteMap, either by contributing to
		s1 = s1<<11 ^ s1>>5 ^ B(v2^as>>11^s1)<<27     // the lower bits of the index, or in the upper bits that
		_ = 0                                         // move the access further in the map.
		hs[idx] = s1 ^ as ^ hs[idx]<<7 ^ hs[idx]>>13  //
		_ = 0                                         // We also pay attention not only to where the ByteMap bits
		as = as<<17 ^ as>>5 ^ s1 ^ B(as^s1>>27^v2)<<3 // are applied, but what bits we use in the indexing of
		as = as<<13 ^ as>>3 ^ B(as^s1)<<7             // the ByteMap
		as = as<<15 ^ as>>7 ^ B(as>>7^s1)<<11         //
		as = as<<9 ^ as>>11 ^ B(v2^as^s1)<<3          // Tests run against this set of shifts show that the
		_ = 0                                         // bytes pulled from the ByteMap are evenly distributed
		s1 = s1<<7 ^ s1>>27 ^ as ^ B(as>>3)<<13       // over possible byte values (0-255) and indexes into
		s1 = s1<<3 ^ s1>>13 ^ B(s1^v2)<<11            // the ByteMap are also evenly distributed, and the
		s1 = s1<<8 ^ s1>>11 ^ B(as^s1>>11)<<9         // deltas between bytes provided map to a curve expected
		s1 = s1<<6 ^ s1>>9 ^ B(v2^as^s1)<<3           // (fewer maximum and minimum deltas, and most deltas around
		_ = 0                                         // zero.
		as = as<<23 ^ as>>3 ^ s1 ^ B(as^v2^s1>>3)<<7
		as = as<<17 ^ as>>7 ^ B(as^s1>>3)<<5
		as = as<<13 ^ as>>5 ^ B(as>>5^s1)<<1
		as = as<<11 ^ as>>1 ^ B(v2^as^s1)<<7

		s1 = s1<<5 ^ s1>>3 ^ as ^ B(as>>7^s1>>3)<<6
		s1 = s1<<8 ^ s1>>6 ^ B(s1^v2)<<11
		s1 = s1<<11 ^ s1>>11 ^ B(as^s1>>11)<<5
		s1 = s1<<7 ^ s1>>5 ^ B(v2^as>>7^as^s1)<<17

		s2 = s2<<3 ^ s2>>17 ^ s1 ^ B(as^s2>>5^v2)<<13
		s2 = s2<<6 ^ s2>>13 ^ B(s2)<<11
		s2 = s2<<11 ^ s2>>11 ^ B(as^s1^s2>>11)<<23
		s2 = s2<<4 ^ s2>>23 ^ B(v2^as>>8^as^s2>>10)<<1

		s1 = s2<<3 ^ s2>>1 ^ hs[idx] ^ v2
		as = as<<9 ^ as>>7 ^ s1>>1 ^ B(s2>>1^hs[idx])<<5

		s1, s2, s3 = s3, s1, s2
	}

	idx := uint64(0)
	// Fast spin to prevent caching state
	for _, v2 := range src {
		if idx >= lx.HashSize { // Use an if to avoid modulo math
			idx = 0
		}
		faststep(uint64(v2), idx)
		idx++
	}

	idx = 0
	// Actual work to compute the hash
	for _, v2 := range src {
		if idx >= lx.HashSize { // Use an if to avoid modulo math
			idx = 0
		}
		step(uint64(v2), idx)
		idx++
	}

	// Reduction pass
	// Done by Interating over hs[] to produce the bytes[] hash
	//
	// At this point, we have HBits of state in hs.  We need to reduce them down to a byte,
	// And we do so by doing a bit more bitwise math, and mapping the values through our byte map.

	bytes := make([]byte, lx.HashSize)
	// Roll over all the hs (one int64 value for every byte in the resulting hash) and reduce them to byte values
	for i := len(hs) - 1; i >= 0; i-- {
		step(hs[i], uint64(i))      // Step the hash functions and then
		bytes[i] = b(as) ^ b(hs[i]) // Xor two resulting sequences
	}

	// Return the resulting hash
	return bytes
}

// Hash takes the arbitrary input and returns the resulting hash of length HashSize
// Does not use anonymous functions
func (lx LXRHash) Hash(src []byte) []byte {
	// Keep the byte intermediate results as int64 values until reduced.
	hs := make([]uint64, lx.HashSize)
	// as accumulates the state as we walk through applying the source data through the lookup map
	// and combine it with the state we are building up.
	var as = lx.Seed
	// We keep a series of states, and roll them along through each byte of source processed.
	var s1, s2, s3 uint64
	// Since MapSize is specified in bits, the index mask is the size-1
	mk := lx.MapSize - 1

	idx := uint64(0)
	// Fast spin to prevent caching state
	for _, v2 := range src {
		if idx >= lx.HashSize { // Use an if to avoid modulo math
			idx = 0
		}

		as, s1, s2, s3 = lx.fastStepf(uint64(v2), as, s1, s2, s3, idx, hs)
		idx++
	}

	idx = 0
	// Actual work to compute the hash
	for _, v2 := range src {
		if idx >= lx.HashSize { // Use an if to avoid modulo math
			idx = 0
		}

		as, s1, s2, s3 = lx.stepf(as, s1, s2, s3, uint64(v2), hs, idx, mk)
		idx++
	}

	// Reduction pass
	// Done by Interating over hs[] to produce the bytes[] hash
	//
	// At this point, we have HBits of state in hs.  We need to reduce them down to a byte,
	// And we do so by doing a bit more bitwise math, and mapping the values through our byte map.

	bytes := make([]byte, lx.HashSize)
	// Roll over all the hs (one int64 value for every byte in the resulting hash) and reduce them to byte values
	for i := len(hs) - 1; i >= 0; i-- {
		as, s1, s2, s3 = lx.stepf(as, s1, s2, s3, uint64(hs[i]), hs, uint64(i), mk)
		bytes[i] = lx.ByteMap[as&mk] ^ lx.ByteMap[hs[i]&mk] // Xor two resulting sequences
	}

	// Return the resulting hash
	return bytes
}

package app

import (
	"fmt"

	"github.com/skycoin/dmsg/cipher"
)

func ExamplePacket() {
	pk := cipher.PubKey{}
	addr := Addr{pk, 0}
	loopAddr := LoopAddr{0, addr}

	fmt.Println(addr.Network())
	fmt.Printf("%v\n", addr)
	fmt.Printf("%v\n", loopAddr)

	//Output: skywire
	// {000000000000000000000000000000000000000000000000000000000000000000 0}
	// {0 {000000000000000000000000000000000000000000000000000000000000000000 0}}
}

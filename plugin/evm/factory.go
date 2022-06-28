// (c) 2019-2020, Axia Systems, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/axiacoin/axia-network-v2/ids"
	"github.com/axiacoin/axia-network-v2/snow"
	"github.com/axiacoin/axia-network-v2/vms"
)

var (
	// ID this VM should be referenced by
	ID = ids.ID{'e', 'v', 'm'}

	_ vms.Factory = &Factory{}
)

type Factory struct{}

func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{}, nil
}

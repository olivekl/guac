//
// Copyright 2023 The GUAC Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inmem

import (
	"context"
	"strconv"
	"strings"

	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/guacsec/guac/pkg/assembler/graphql/model"
)

// TODO: convert to unit test
// func registerAllGHSA(client *demoClient) {
// 	ctx := context.Background()

// 	inputs := []model.GHSAInputSpec{{
// 		GhsaID: "GHSA-h45f-rjvw-2rv2",
// 	}, {
// 		GhsaID: "GHSA-xrw3-wqph-3fxg",
// 	}, {
// 		GhsaID: "GHSA-8v4j-7jgf-5rg9",
// 	}}
// 	for _, input := range inputs {
// 		_, err := client.IngestGhsa(ctx, &input)
// 		if err != nil {
// 			log.Printf("Error in ingesting: %v\n", err)
// 		}
// 	}
// }

// Internal data: osv
type ghsaMap map[string]*ghsaNode
type ghsaNode struct {
	id               uint32
	ghsaID           string
	certifyVulnLinks []uint32
	equalVulnLinks   []uint32
	vexLinks         []uint32
}

func (n *ghsaNode) ID() uint32 { return n.id }

func (n *ghsaNode) Neighbors() []uint32 {
	out := make([]uint32, 0, len(n.certifyVulnLinks)+len(n.equalVulnLinks)+len(n.vexLinks))
	out = append(out, n.certifyVulnLinks...)
	out = append(out, n.equalVulnLinks...)
	out = append(out, n.vexLinks...)
	return out
}

func (n *ghsaNode) BuildModelNode(c *demoClient) (model.Node, error) {
	return c.buildGhsaResponse(n.id, nil)
}

// certifyVulnerability back edges
func (n *ghsaNode) setVulnerabilityLinks(id uint32) {
	n.certifyVulnLinks = append(n.certifyVulnLinks, id)
}

// isVulnerability back edges
func (n *ghsaNode) setEqualVulnLinks(id uint32) {
	n.equalVulnLinks = append(n.equalVulnLinks, id)
}

// certifyVexStatement back edges
func (n *ghsaNode) setVexLinks(id uint32) {
	n.vexLinks = append(n.vexLinks, id)
}

// Ingest GHSA
func (c *demoClient) IngestGhsa(ctx context.Context, input *model.GHSAInputSpec) (*model.Ghsa, error) {
	return c.ingestGhsa(ctx, input, true)
}

func (c *demoClient) ingestGhsa(ctx context.Context, input *model.GHSAInputSpec, readOnly bool) (*model.Ghsa, error) {
	lock(&c.m, readOnly)
	defer unlock(&c.m, readOnly)

	ghsaID := strings.ToLower(input.GhsaID)
	ghsaIDStruct, hasGhsaID := c.ghsas[ghsaID]
	if !hasGhsaID {
		if readOnly {
			c.m.RUnlock()
			g, err := c.ingestGhsa(ctx, input, false)
			c.m.RLock() // relock so that defer unlock does not panic
			return g, err
		}
		ghsaIDStruct = &ghsaNode{
			id:     c.getNextID(),
			ghsaID: ghsaID,
		}
		c.index[ghsaIDStruct.id] = ghsaIDStruct
		c.ghsas[ghsaID] = ghsaIDStruct
	}

	// build return GraphQL type
	return c.buildGhsaResponse(ghsaIDStruct.id, nil)
}

// Query GHSA
func (c *demoClient) Ghsa(ctx context.Context, filter *model.GHSASpec) ([]*model.Ghsa, error) {
	c.m.RLock()
	defer c.m.RUnlock()
	if filter != nil && filter.ID != nil {
		id, err := strconv.ParseUint(*filter.ID, 10, 32)
		if err != nil {
			return nil, err
		}
		ghsa, err := c.buildGhsaResponse(uint32(id), filter)
		if err != nil {
			return nil, err
		}
		return []*model.Ghsa{ghsa}, nil
	}
	out := []*model.Ghsa{}
	if filter != nil && filter.GhsaID != nil {
		ghsaNode, hasGhsaIDNode := c.ghsas[strings.ToLower(*filter.GhsaID)]
		if hasGhsaIDNode {
			out = append(out, &model.Ghsa{
				ID:     nodeID(ghsaNode.id),
				GhsaID: ghsaNode.ghsaID,
			})
		}
	} else {
		for _, ghsaNode := range c.ghsas {
			out = append(out, &model.Ghsa{
				ID:     nodeID(ghsaNode.id),
				GhsaID: ghsaNode.ghsaID,
			})
		}
	}
	return out, nil
}

// Builds a model.Ghsa to send as GraphQL response, starting from id.
// The optional filter allows restricting output (on selection operations).
func (c *demoClient) buildGhsaResponse(id uint32, filter *model.GHSASpec) (*model.Ghsa, error) {
	if filter != nil && filter.ID != nil {
		filteredID, err := strconv.ParseUint(*filter.ID, 10, 32)
		if err != nil {
			return nil, err
		}
		if uint32(filteredID) != id {
			return nil, nil
		}
	}

	node, ok := c.index[id]
	if !ok {
		return nil, gqlerror.Errorf("ID does not match existing node")
	}

	var ghsa *model.Ghsa
	if ghsaNode, ok := node.(*ghsaNode); ok {
		if filter != nil && noMatch(toLower(filter.GhsaID), ghsaNode.ghsaID) {
			return nil, nil
		}
		ghsa = &model.Ghsa{
			ID:     nodeID(ghsaNode.id),
			GhsaID: ghsaNode.ghsaID,
		}
	}

	return ghsa, nil
}

func (c *demoClient) exactGHSA(filter *model.GHSASpec) (*ghsaNode, error) {
	if filter == nil {
		return nil, nil
	}
	if filter.ID != nil {
		id64, err := strconv.ParseUint(*filter.ID, 10, 32)
		if err != nil {
			return nil, err
		}
		id := uint32(id64)
		if node, ok := c.index[id]; ok {
			if g, ok := node.(*ghsaNode); ok {
				return g, nil
			}
		}
	}
	if filter.GhsaID != nil {
		if node, ok := c.ghsas[strings.ToLower(*filter.GhsaID)]; ok {
			return node, nil
		}
	}
	return nil, nil
}

func getGhsaIDFromInput(c *demoClient, input model.GHSAInputSpec) (uint32, error) {
	ghsaID := strings.ToLower(input.GhsaID)

	ghsaIDStruct, hasGhsaID := c.ghsas[ghsaID]
	if !hasGhsaID {
		return 0, gqlerror.Errorf("ghsa id \"%s\" not found", input.GhsaID)
	}

	return ghsaIDStruct.id, nil
}

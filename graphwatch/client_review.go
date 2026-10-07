package main

import (
	"context"
	"encoding/json"
	"time"
)

// apiReviewItem is one human review item (GET /graphs/{id}/review). Kind and
// State are kept verbatim: the service owns their vocabulary.
type apiReviewItem struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	NodeID    string          `json:"node_id"`
	RunID     string          `json:"run_id"`
	State     string          `json:"state"`
	Reason    string          `json:"reason"`
	CreatedAt time.Time       `json:"created_at"`
	Raw       json.RawMessage `json:"-"`
}

// reviewItems is read-only: the resolve endpoint is never called from here.
func (c *client) reviewItems(ctx context.Context, graphID string) ([]apiReviewItem, error) {
	raw, err := page[json.RawMessage](ctx, c, "/graphs/"+graphID+"/review")
	if err != nil {
		return nil, err
	}
	items := make([]apiReviewItem, 0, len(raw))
	for _, r := range raw {
		var it apiReviewItem
		if err := json.Unmarshal(r, &it); err != nil {
			return nil, err
		}
		it.Raw = r
		items = append(items, it)
	}
	return items, nil
}

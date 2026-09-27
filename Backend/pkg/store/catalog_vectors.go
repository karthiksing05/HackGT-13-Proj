package store

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// embeddingBatch bounds the ids per catalog query when loading vectors.
const embeddingBatch = 200

// Embeddings loads the stored 1024-d vectors of the given catalog documents
// by hex id, for taste matching. Malformed ids and documents without a
// vector are absent from the map. This is the one Catalog read that loads
// vectors, and only the vectors.
func (c Catalog) Embeddings(ctx context.Context, catalog string, ids []string) (map[string][]float64, error) {
	oids := objectIDs(ids)
	out := make(map[string][]float64, len(oids))
	for start := 0; start < len(oids); start += embeddingBatch {
		chunk := oids[start:min(start+embeddingBatch, len(oids))]
		cursor, err := c.coll(catalog).Find(ctx, bson.M{"_id": bson.M{"$in": chunk}},
			options.Find().SetProjection(bson.M{"embedding": 1}))
		if err != nil {
			return nil, err
		}
		var rows []struct {
			ID        bson.ObjectID `bson:"_id"`
			Embedding []float64     `bson:"embedding"`
		}
		if err := cursor.All(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if len(r.Embedding) > 0 {
				out[r.ID.Hex()] = r.Embedding
			}
		}
	}
	return out, nil
}

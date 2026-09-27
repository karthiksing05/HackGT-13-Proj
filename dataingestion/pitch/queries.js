// Read-only checks of freetime.pitch_activities for mongosh (the same checks, and more, run
// automatically with `python -m pitch.validate --mongo`).
//
//   mongosh "mongodb://127.0.0.1:27017/freetime" dataingestion/pitch/queries.js
const c = db.getSiblingDB("freetime").pitch_activities;
const tz = "America/New_York";

print("documents:", c.countDocuments({}), "(want 250)");
// The planner's price reading (Backend/pkg/planner/price.go): a known min > 0 is paid, min 0 is free.
print("paid:", c.countDocuments({ "price.min": { $gt: 0 } }), "(want 150)",
      "free:", c.countDocuments({ "price.min": 0, "price.isFree": true }), "(want 100)",
      "unknown:", c.countDocuments({ $or: [{ price: null }, { "price.min": null }] }), "(want 0)");

print("\nkind x category");
c.aggregate([{ $group: { _id: { kind: "$kind", category: "$category" }, n: { $sum: 1 } } },
             { $sort: { "_id.kind": 1, n: -1 } }]).forEach(r => print(" ", r._id.kind, r._id.category, r.n));

print("\nevents per local day");
c.aggregate([{ $match: { kind: "event" } },
             { $group: { _id: { $dateToString: { format: "%Y-%m-%d %a", date: "$start", timezone: tz } }, n: { $sum: 1 } } },
             { $sort: { _id: 1 } }]).forEach(r => print(" ", r._id, r.n));

print("\nintegrity (all want 0)");
print("  events with end <= start:", c.countDocuments({ kind: "event", $expr: { $lte: ["$end", "$start"] } }));
print("  events without start/end:", c.countDocuments({ kind: "event", $or: [{ start: null }, { end: null }] }));
print("  places without weeklyHours:", c.countDocuments({ kind: "place", $or: [{ weeklyHours: null }, { weeklyHours: { $size: 0 } }] }));
print("  places rated under 4 (not hikes):", c.countDocuments({ kind: "place", category: { $ne: "hike" }, $or: [{ rating: null }, { rating: { $lt: 4 } }] }));
print("  isFree disagreeing with min:", c.countDocuments({ $or: [{ "price.isFree": true, "price.min": { $gt: 0 } }, { "price.isFree": false, "price.min": 0 }] }));
print("  max < min:", c.countDocuments({ $expr: { $lt: ["$price.max", "$price.min"] } }));
print("  ticketUrl or imageUrl set:", c.countDocuments({ $or: [{ ticketUrl: { $ne: null } }, { imageUrl: { $ne: null } }] }));
print("  sources other than pitch:", c.countDocuments({ "sources.name": { $ne: "pitch" } }));
print("  duplicate sourceKeys:", c.aggregate([{ $unwind: "$sourceKeys" }, { $group: { _id: "$sourceKeys", n: { $sum: 1 } } }, { $match: { n: { $gt: 1 } } }]).toArray().length);
print("  expiresAt != end + 6h:", c.countDocuments({ kind: "event", $expr: { $ne: ["$expiresAt", { $add: ["$end", 6 * 3600 * 1000] }] } }));

print("\nembedding texts and vectors");
print("  with embeddingText:", c.countDocuments({ embeddingTextHash: { $type: "string" } }), "(want 250)");
print("  with a vector:", c.countDocuments({ embedding: { $exists: true } }), "(0 until embed_missing runs over this collection)");

print("\nTTL indexes (want none):");
printjson(c.getIndexes().filter(i => "expireAfterSeconds" in i));

// The pitch window: events that overlap Sunday 13:00-17:00 local, and fixed starts inside it.
const from = new Date("2026-09-27T17:00:00Z"), to = new Date("2026-09-27T21:00:00Z");
print("\nSun 1-5 PM: drop-ins overlapping:", c.countDocuments({ kind: "event", attendance: "drop_in", start: { $lt: to }, end: { $gt: from } }),
      "fixed starts inside:", c.countDocuments({ kind: "event", attendance: "fixed_start", start: { $gte: from, $lt: to } }));

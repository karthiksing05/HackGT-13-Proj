package main

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Calendar mode (--calendar @handle,…) gives real accounts a Georgia Tech
// fall 2026 week in calendar_events: classes with rooms, a recitation or
// lab, homework blocks, one or two extras that fit their story, and a few
// one-offs (a midterm, a due date, office hours). Recurring events are one
// document per occurrence sharing a SeriesID, from the first day of class
// to the last, skipping the holidays. Everything carries seed:
// "calendar-v1", source "seed" and fixed ids and times, so a rerun rewrites
// the same documents and --remove deletes exactly them.
const calendarTag = "calendar-v1"

var (
	nyTZ          = mustLoad(atlantaTZ)
	semesterStart = time.Date(2026, time.August, 17, 0, 0, 0, 0, nyTZ) // Monday, first day of class
	semesterEnd   = time.Date(2026, time.December, 11, 0, 0, 0, 0, nyTZ)
	// calendarStamp is when the schedules were "added": fixed, so every run
	// writes the same bytes.
	calendarStamp = time.Date(2026, time.August, 10, 16, 0, 0, 0, time.UTC)
	// noClasses are Labor Day, fall break and Thanksgiving.
	noClasses = map[string]bool{"2026-09-07": true, "2026-10-12": true, "2026-10-13": true,
		"2026-11-25": true, "2026-11-26": true, "2026-11-27": true}
)

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("seed: " + err.Error())
	}
	return loc
}

// weekly is a recurring block: the weekdays it meets, its local time and length.
type weekly struct {
	key, title, location string
	days                 []time.Weekday
	hour, minute, length int  // start and length in minutes
	class                bool // a class or recitation: none on holidays
}

// oneOff is a single block on a date.
type oneOff struct {
	key, title, location string
	date                 string // YYYY-MM-DD
	hour, minute, length int
}

// schedule is one person's semester.
type schedule struct {
	about   string
	weekly  []weekly
	oneOffs []oneOff
}

var (
	mwf = []time.Weekday{time.Monday, time.Wednesday, time.Friday}
	tth = []time.Weekday{time.Tuesday, time.Thursday}
)

// schedules are the three semesters, by history story: a CS student who
// plays in a band, an industrial engineer in the run and climbing clubs, and
// an architecture student who volunteers at a gallery. Weekday late
// afternoons, most evenings and most of the weekend stay free.
var schedules = map[string]schedule{
	"nightlife": {
		about: "CS, with band practice on Thursday evenings",
		weekly: []weekly{
			{key: "cs3510", title: "CS 3510 Design & Analysis of Algorithms", location: "Klaus 1443", days: tth, hour: 9, minute: 30, length: 75, class: true},
			{key: "cs2200", title: "CS 2200 Computer Systems & Networks", location: "Howey L2", days: tth, hour: 12, minute: 30, length: 75, class: true},
			{key: "math3012", title: "MATH 3012 Applied Combinatorics", location: "Skiles 243", days: mwf, hour: 10, length: 50, class: true},
			{key: "cs2340", title: "CS 2340 Objects & Design", location: "CCB 16", days: mwf, hour: 13, length: 50, class: true},
			{key: "cs2200rec", title: "CS 2200 recitation", location: "Klaus 1447", days: []time.Weekday{time.Wednesday}, hour: 14, length: 50, class: true},
			{key: "cs3510hw", title: "CS 3510 HW", location: "Crosland Tower", days: []time.Weekday{time.Tuesday}, hour: 19, length: 120},
			{key: "math3012ps", title: "MATH 3012 problem set", location: "Klaus atrium", days: []time.Weekday{time.Sunday}, hour: 14, length: 120},
			{key: "cs2340proj", title: "CS 2340 project team", location: "CULC 262", days: []time.Weekday{time.Monday}, hour: 16, length: 90},
			{key: "band", title: "Band practice", location: "Couch Building 213", days: []time.Weekday{time.Thursday}, hour: 19, minute: 30, length: 120},
		},
		oneOffs: []oneOff{
			{key: "cs3510mid1", title: "CS 3510 Midterm 1", location: "Klaus 1443", date: "2026-09-24", hour: 9, minute: 30, length: 75},
			{key: "cs2200exam1", title: "CS 2200 Exam 1", location: "Howey L1", date: "2026-10-07", hour: 18, minute: 30, length: 90},
			{key: "cs2340m2", title: "CS 2340 Milestone 2 due", location: "Canvas", date: "2026-10-09", hour: 23, length: 59},
			{key: "cs3510oh", title: "CS 3510 office hours", location: "Klaus 2100", date: "2026-09-30", hour: 15, length: 60},
			{key: "math3012mid", title: "MATH 3012 Midterm", location: "Skiles 243", date: "2026-10-16", hour: 10, length: 50},
		},
	},
	"outdoors": {
		about: "industrial engineering, with early run club and Saturday climbing",
		weekly: []weekly{
			{key: "isye3133", title: "ISYE 3133 Engineering Optimization", location: "Groseclose 402", days: tth, hour: 11, length: 75, class: true},
			{key: "isye2027", title: "ISYE 2027 Probability with Applications", location: "Instructional Center 213", days: mwf, hour: 9, length: 50, class: true},
			{key: "mgt3150", title: "MGT 3150 Principles of Management", location: "Scheller 102", days: tth, hour: 14, length: 75, class: true},
			{key: "econ2106", title: "ECON 2106 Principles of Microeconomics", location: "Clough 152", days: mwf, hour: 12, length: 50, class: true},
			{key: "isye2027rec", title: "ISYE 2027 recitation", location: "Skiles 170", days: []time.Weekday{time.Friday}, hour: 11, length: 50, class: true},
			{key: "isye2027ps", title: "ISYE 2027 problem set", location: "Crosland Tower", days: []time.Weekday{time.Monday}, hour: 19, length: 120},
			{key: "isye3133hw", title: "ISYE 3133 HW", location: "Clough Commons", days: []time.Weekday{time.Wednesday}, hour: 19, length: 90},
			{key: "studygroup", title: "Study group", location: "Crosland Tower 4th floor", days: []time.Weekday{time.Sunday}, hour: 15, length: 120},
			{key: "runclub", title: "Run club", location: "Tech Green", days: tth, hour: 6, minute: 30, length: 60},
			{key: "climbing", title: "Climbing club", location: "CRC climbing wall", days: []time.Weekday{time.Saturday}, hour: 9, length: 120},
		},
		oneOffs: []oneOff{
			{key: "isye2027mid", title: "ISYE 2027 Midterm", location: "Clough 152", date: "2026-10-01", hour: 18, minute: 30, length: 90},
			{key: "mgt3150case", title: "MGT 3150 case study due", location: "Canvas", date: "2026-10-16", hour: 23, length: 59},
			{key: "isye3133oh", title: "ISYE 3133 office hours", location: "Groseclose 226", date: "2026-09-29", hour: 16, length: 60},
			{key: "isye3133mid", title: "ISYE 3133 Midterm", location: "Groseclose 402", date: "2026-10-22", hour: 11, length: 75},
		},
	},
	"arts": {
		about: "architecture, with studio time and gallery volunteering",
		weekly: []weekly{
			{key: "lmc2500", title: "LMC 2500 Introduction to Film", location: "Skiles 002", days: tth, hour: 9, minute: 30, length: 75, class: true},
			{key: "arch2111", title: "ARCH 2111 Visual Arts I", location: "Hinman 105", days: mwf, hour: 10, length: 50, class: true},
			{key: "coa2242", title: "COA 2242 History of Art II", location: "Clough 131", days: tth, hour: 14, length: 75, class: true},
			{key: "math1554", title: "MATH 1554 Linear Algebra", location: "Skiles 249", days: mwf, hour: 12, length: 50, class: true},
			{key: "math1554rec", title: "MATH 1554 recitation", location: "Skiles 170", days: []time.Weekday{time.Tuesday}, hour: 12, minute: 30, length: 50, class: true},
			{key: "math1554hw", title: "MATH 1554 homework", location: "Library", days: []time.Weekday{time.Tuesday}, hour: 19, length: 120},
			{key: "arch2111sketch", title: "ARCH 2111 sketchbook", location: "West Architecture", days: []time.Weekday{time.Wednesday}, hour: 19, length: 90},
			{key: "coa2242reading", title: "COA 2242 reading", location: "Architecture Library", days: []time.Weekday{time.Sunday}, hour: 13, length: 120},
			{key: "studio", title: "Studio time", location: "West Architecture 351", days: []time.Weekday{time.Monday, time.Wednesday}, hour: 14, length: 120},
			{key: "gallery", title: "Gallery volunteer shift", location: "MODA", days: []time.Weekday{time.Saturday}, hour: 11, length: 180},
		},
		oneOffs: []oneOff{
			{key: "math1554mid", title: "MATH 1554 Midterm 1", location: "Skiles 249", date: "2026-10-02", hour: 12, length: 50},
			{key: "arch2111review", title: "ARCH 2111 portfolio review", location: "Hinman 105", date: "2026-10-21", hour: 13, length: 120},
			{key: "coa2242oh", title: "COA 2242 office hours", location: "Hinman 207", date: "2026-09-24", hour: 15, minute: 30, length: 60},
			{key: "lmc2500essay", title: "LMC 2500 essay due", location: "Canvas", date: "2026-10-30", hour: 23, length: 59},
		},
	},
}

func calendarID(uid, key string) string { return fmt.Sprintf("%s-%s-%s", calendarTag, uid, key) }

// calendarEvents are one person's documents for their story's schedule.
func calendarEvents(uid string, sch schedule) []models.CalendarEvent {
	var out []models.CalendarEvent
	event := func(id, series, title, location string, start time.Time, minutes int) models.CalendarEvent {
		return models.CalendarEvent{ID: id, UserID: uid, Title: title, Start: start.UTC(), End: start.Add(time.Duration(minutes) * time.Minute).UTC(),
			Location: location, Source: "seed", SeriesID: series, Seed: calendarTag, CreatedAt: calendarStamp, UpdatedAt: calendarStamp}
	}
	for _, w := range sch.weekly {
		series := calendarID(uid, w.key)
		for day := semesterStart; !day.After(semesterEnd); day = day.AddDate(0, 0, 1) {
			key := day.Format("2006-01-02")
			if !containsDay(w.days, day.Weekday()) || (w.class && noClasses[key]) {
				continue
			}
			start := time.Date(day.Year(), day.Month(), day.Day(), w.hour, w.minute, 0, 0, nyTZ)
			out = append(out, event(series+"-"+day.Format("20060102"), series, w.title, w.location, start, w.length))
		}
	}
	for _, o := range sch.oneOffs {
		day, err := time.ParseInLocation("2006-01-02", o.date, nyTZ)
		if err != nil {
			panic("seed: " + err.Error())
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), o.hour, o.minute, 0, 0, nyTZ)
		out = append(out, event(calendarID(uid, o.key), "", o.title, o.location, start, o.length))
	}
	return out
}

func containsDay(days []time.Weekday, d time.Weekday) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

// runCalendar is --calendar: plan (and with --apply write, or with
// --remove delete) each person's semester.
func runCalendar(ctx context.Context, st *store.Store, o Options, p printer) error {
	people, err := resolveHistory(ctx, st, o.Calendar)
	if err != nil {
		return err
	}
	for _, hp := range people {
		if hp.backup, err = loadBackup(ctx, st, hp.uid()); err != nil {
			return err
		}
	}
	if err := pickFlavors(people); err != nil {
		return err
	}
	coll := st.Collection(store.CollCalendarEvents)
	for _, hp := range people {
		uid := hp.uid()
		mine := bson.M{fieldSeed: calendarTag, "userId": uid}
		have, err := coll.CountDocuments(ctx, mine)
		if err != nil {
			return err
		}
		p.f("")
		p.f("@%s · %s · id %s", hp.user.Username, hp.user.Name, uid)
		if o.Remove {
			p.f("  calendar events of this seed: %d", have)
			if o.Apply {
				if _, err := coll.DeleteMany(ctx, mine); err != nil {
					return err
				}
				if _, err := st.Collection(collBackups).DeleteOne(ctx, bson.M{"_id": calendarMarkerID(uid)}); err != nil {
					return err
				}
			}
			continue
		}
		sch := schedules[hp.flavor.key]
		events := calendarEvents(uid, sch)
		p.f("  Semester (%s story, %s): %d events, %d already there, Aug 17 to Dec 11, no classes on Labor Day, fall break or Thanksgiving",
			hp.flavor.key, sch.about, len(events), have)
		for _, w := range sch.weekly {
			var days []string
			for _, d := range w.days {
				days = append(days, d.String()[:3])
			}
			start := time.Date(2026, 1, 1, w.hour, w.minute, 0, 0, nyTZ)
			p.f("    %-12s %s–%s  %s · %s", strings.Join(days, " "), start.Format("3:04"), start.Add(time.Duration(w.length)*time.Minute).Format("3:04 PM"),
				w.title, w.location)
		}
		for _, oo := range sch.oneOffs {
			p.f("    once %s  %s · %s", oo.date, oo.title, oo.location)
		}
		if o.Apply {
			if err := writeCalendar(ctx, st, uid, events); err != nil {
				return fmt.Errorf("@%s: %w", hp.user.Username, err)
			}
			// The mark --reset reads to know the calendar is part of their baseline.
			if _, err := st.Collection(collBackups).UpdateOne(ctx, bson.M{"_id": calendarMarkerID(uid)},
				bson.M{"$set": bson.M{fieldSeed: calendarTag, fieldFor: uid, "flavor": hp.flavor.key}}, options.UpdateOne().SetUpsert(true)); err != nil {
				return err
			}
		}
	}
	p.f("")
	switch {
	case !o.Apply && o.Remove:
		p.f("Dry run: nothing was deleted. Run again with --calendar … --remove --apply.")
	case !o.Apply:
		p.f("Dry run: nothing was written. Run again with --apply to write it.")
	default:
		p.f("Done.")
	}
	return nil
}

// writeCalendar replaces the person's seeded events with events (fixed ids
// and times: a rerun writes the same bytes) and drops any of theirs from an
// older schedule.
func writeCalendar(ctx context.Context, st *store.Store, uid string, events []models.CalendarEvent) error {
	coll := st.Collection(store.CollCalendarEvents)
	ids := make([]string, 0, len(events))
	var writes []mongo.WriteModel
	for i := range events {
		ids = append(ids, events[i].ID)
		writes = append(writes, mongo.NewReplaceOneModel().SetFilter(bson.M{"_id": events[i].ID, fieldSeed: calendarTag}).
			SetReplacement(&events[i]).SetUpsert(true))
	}
	for start := 0; start < len(writes); start += 500 {
		if _, err := coll.BulkWrite(ctx, writes[start:min(start+500, len(writes))], options.BulkWrite().SetOrdered(false)); err != nil {
			return fmt.Errorf("write calendar events: %w", err)
		}
	}
	_, err := coll.DeleteMany(ctx, bson.M{fieldSeed: calendarTag, "userId": uid, "_id": bson.M{"$nin": list(ids)}})
	return err
}

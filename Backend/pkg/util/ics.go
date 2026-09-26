package util

import (
	"fmt"
	"strings"
	"time"
)

type ICSEvent struct {
	UID         string
	Start       time.Time
	End         time.Time
	Summary     string
	Description string
	Location    string
	URL         string
	Status      string
}

func FormatICSTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func EscapeICSText(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\r\n", "\\n")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\n")
	return s
}

// BuildICSCalendar generates RFC 5545 compliant iCalendar string
func BuildICSCalendar(calName string, events []ICSEvent) string {
	var sb strings.Builder
	nowStr := FormatICSTime(time.Now())

	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//SideQuestz//SideQuestz Calendar 1.0//EN\r\n")
	sb.WriteString("CALSCALE:GREGORIAN\r\n")
	sb.WriteString("METHOD:PUBLISH\r\n")
	sb.WriteString(fmt.Sprintf("X-WR-CALNAME:%s\r\n", EscapeICSText(calName)))
	sb.WriteString("X-WR-TIMEZONE:America/New_York\r\n")
	sb.WriteString("REFRESH-INTERVAL;VALUE=DURATION:PT1H\r\n")
	sb.WriteString("X-PUBLISHED-TTL:PT1H\r\n")

	for _, e := range events {
		sb.WriteString("BEGIN:VEVENT\r\n")
		sb.WriteString(fmt.Sprintf("UID:%s\r\n", e.UID))
		sb.WriteString(fmt.Sprintf("DTSTAMP:%s\r\n", nowStr))
		sb.WriteString(fmt.Sprintf("DTSTART:%s\r\n", FormatICSTime(e.Start)))
		if !e.End.IsZero() {
			sb.WriteString(fmt.Sprintf("DTEND:%s\r\n", FormatICSTime(e.End)))
		} else {
			sb.WriteString(fmt.Sprintf("DTEND:%s\r\n", FormatICSTime(e.Start.Add(2*time.Hour))))
		}
		sb.WriteString(fmt.Sprintf("SUMMARY:%s\r\n", EscapeICSText(e.Summary)))
		if e.Description != "" {
			sb.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", EscapeICSText(e.Description)))
		}
		if e.Location != "" {
			sb.WriteString(fmt.Sprintf("LOCATION:%s\r\n", EscapeICSText(e.Location)))
		}
		if e.URL != "" {
			sb.WriteString(fmt.Sprintf("URL:%s\r\n", e.URL))
		}
		status := e.Status
		if status == "" {
			status = "CONFIRMED"
		}
		sb.WriteString(fmt.Sprintf("STATUS:%s\r\n", status))
		sb.WriteString("END:VEVENT\r\n")
	}

	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

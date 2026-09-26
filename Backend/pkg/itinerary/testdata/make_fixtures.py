import json, hashlib, math
from datetime import datetime, timezone, timedelta
from zoneinfo import ZoneInfo

def unwrap(v):
    if isinstance(v, dict) and '$date' in v: return v['$date']
    if isinstance(v, dict) and '$oid' in v: return v['$oid']
    return v

def km(a, b):
    r = math.pi/180
    dla, dlo = (b[1]-a[1])*r, (b[0]-a[0])*r
    h = math.sin(dla/2)**2 + math.cos(a[1]*r)*math.cos(b[1]*r)*math.sin(dlo/2)**2
    return 12742*math.asin(math.sqrt(min(1,h)))

def score(oid):  # deterministic stand-in for ranker output, 0.35..0.95
    return round(0.35 + 0.6*(int(hashlib.sha1(oid.encode()).hexdigest()[:8],16)/0xffffffff), 3)

KEEP = ['kind','city','name','category','venueName','location','start','end','attendance',
        'timezone','weeklyHours','duration','price']

def trim(x):
    out = {k: unwrap(x.get(k)) for k in KEEP}
    out['_id'] = unwrap(x['_id'])
    out['score'] = score(out['_id'])
    d = out.get('duration') or {}
    for k in ('medianMin','p75Min'):   # corrupt trail durations don't fit JSON numbers for Go
        if isinstance(d.get(k), (int,float)) and d[k] > 1e6: d[k] = 1e6
    return out

def parse(s): return datetime.fromisoformat(s.replace('Z','+00:00'))

def day_events(docs, tz, day, lo_h, hi_h):
    lo = datetime(*day, lo_h, tzinfo=ZoneInfo(tz)); hi = lo + timedelta(hours=hi_h-lo_h)
    out = []
    for x in docs:
        if x['kind'] != 'event' or not x.get('start'): continue
        s = parse(unwrap(x['start'])); e = parse(unwrap(x['end'])) if x.get('end') else s + timedelta(hours=3)
        if s < hi and e > lo: out.append(trim(x))
    return out

src = 'dataingestion/out/'
atl = json.load(open(src+'final_atlanta.json'))
tech = [-84.3890, 33.7766]
ev = day_events(atl, 'America/New_York', (2026,9,26), 12, 24)
pl = sorted([x for x in atl if x['kind']=='place' and km(tech, x['location']['coordinates']) <= 6],
            key=lambda x: -(x.get('rating') or 0))[:60]
json.dump(ev + [trim(x) for x in pl], open('Backend/pkg/itinerary/testdata/atlanta_2026-09-26.json','w'), indent=0)
print('atlanta', len(ev), 'events', len(pl), 'places')

nyc = json.load(open(src+'final_nyc.json'))
ev = day_events(nyc, 'America/New_York', (2026,9,26), 12, 24)
json.dump(ev, open('Backend/pkg/itinerary/testdata/nyc_2026-09-26.json','w'), indent=0)
print('nyc', len(ev), 'events')

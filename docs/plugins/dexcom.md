# dexcom plugin

Shows the glucose readings of a Dexcom G7 sensor. The data comes from the Dexcom Share service, the cloud the G7 app uploads to. The sensor itself speaks a proprietary protocol and is not read directly, and Dexcom's developer API is only open to partners, so this plugin takes the path xDrip+, Nightscout's bridge and pydexcom use: the Share endpoints (`share2.dexcom.com` in Europe, `shareous1.dexcom.com` in the US). Those endpoints are not documented for third parties and can change.

Share has to be switched on in the app (Connections → Share). Nothing reaches the cloud otherwise, and the plugin gets no readings while the phone is offline.

## Configuration

| Key | Description |
|---|---|
| `account_name` | Account name (the e-mail address of the Dexcom account); exchanged for the account ID once and then kept in the state |
| `account_id` | The account ID instead, a GUID, if you have it. Give `account_name` or `account_id`, not both |
| `password` | The account password, preferably via `${DEXCOM_PASSWORD}` |
| `app_id` | Required: the application ID (a GUID) of the app that created the share |
| `region` | Required: `eu` or `us`. The account may live on another host than the country it is used in, so the plugin does not guess |
| `refresh` | How often the readings are read, default `5m`, at least `1m` |
| `unit` | `auto` (default), `mg/dL` or `mmol/L`. `auto` takes what the account reports: a reading below 30 is mmol/L |
| `urgent` | Threshold of the urgent low, default 55 mg/dL or 3.1 mmol/L; `0` disables the state |
| `low`, `high` | The target band, default 80 and 280 mg/dL or 4.4 and 15.5 mmol/L |
| `min`, `max` | The scale of the chart, default 40 and 300 mg/dL or 2.2 and 16.7 mmol/L |

The defaults are the numbers of the Dexcom app. Thresholds are read in the unit of the account, so a mmol/L account sets `low: 4.4` rather than `80`.

```yaml
plugins:
  dexcom:
    config:
      account_name: ${DEXCOM_ACCOUNT}
      password: ${DEXCOM_PASSWORD}
      app_id: 00000000-0000-0000-0000-000000000000
      region: eu
```

## Widget `glucose`

The reading large, its trend as an arrow, how it moved over the last 15 minutes, its age, and a bar that shows the target band, the urgent threshold and where the value sits in the scale. Image and text variants.

Outside the band the value is drawn inverted (light on dark), below `urgent` it gets a ring and the word „Dringend!"/“Urgent!”. A reading older than 15 minutes is drawn gray instead of as a current value.

The text variant is one line: `112 mg/dL` and the change, the alarm or the age next to it (`examples/pages/glukose.lcd2004.yaml`).

## Widget `graph`

The readings of the day (`period: day`, the default) or of the last hour (`period: hour`). The band is two labelled lines, and the area the curve spends outside the band is filled, so a spike above `high` or a dip below `low` shows how far it went and how long it lasted. The hour variant marks the five-minute points.

Image displays only.

## Widget `values`

Single values as rows, like the `values` widget of the evcc plugin. The list arg `values` has one check box per entry in the editor:

| Key | Shows |
|---|---|
| `now` | The newest reading with its unit |
| `avg`, `in_band` | Average and share inside the band over the last day |
| `min`, `max` | The lowest and highest reading of the last day |
| `delta` | The change over the last 15 minutes |
| `trend` | The trend as an ASCII arrow |
| `age` | How old the newest reading is |
| `count` | How many readings the day has |
| `band` | The configured target band |

Without `values` all entries are shown; `columns` sets one or two columns. Rows that do not fit the cell are left out, so the order of the list is the order of importance. Image and text variants.

## Data and state

The service keeps one day. The first read takes the whole day; after that the plugin asks for the readings it missed since the last poll (`maxCount` = the missed five-minute values plus one for context, at most 288) and merges them into the stored series, dropping duplicates and anything older than 24 hours. A poll therefore moves a handful of values instead of a day.

The plugin state (ADR 0081) holds the account ID, the session, the time of the last read and the readings of the day, so a restart costs neither a login nor a read, and the first page after a restart has data. A session the service forgot costs one new login; the session is not renewed on every request.

There is no week or month view: the service does not answer more than a day, and the plugin does not keep its own history. Nightscout or Juggluco would be the source for that.

## Privacy

The credentials go only to Dexcom; `${DEXCOM_PASSWORD}` keeps the password out of `paperboy.yaml`. The readings are stored unencrypted under `state/plugins/dexcom/state.json`, readable by the owner of the instance only. The Share API is undocumented and meant for personal use; check what Dexcom allows before sharing the account.

update external_events
set dedupe_key = 'internal:' || source_internal_event_id::text || ':' || type
where source_internal_event_id is not null
  and dedupe_key <> 'internal:' || source_internal_event_id::text || ':' || type;

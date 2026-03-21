DROP TRIGGER IF EXISTS trg_event_subscriptions_updated_at ON event_subscriptions;
DROP TRIGGER IF EXISTS trg_files_updated_at ON files;
DROP TRIGGER IF EXISTS trg_bookmarks_updated_at ON bookmarks;
DROP TRIGGER IF EXISTS trg_usergroups_updated_at ON usergroups;

DROP TABLE IF EXISTS tokens;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS event_subscriptions;
DROP TABLE IF EXISTS file_channels;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS bookmarks;
DROP TABLE IF EXISTS pins;
DROP TABLE IF EXISTS usergroup_members;
DROP TABLE IF EXISTS usergroups;

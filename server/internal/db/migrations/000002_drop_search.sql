drop table if exists search_documents;

delete from projector_checkpoints
where name = 'indexer';

delete from projector_leases
where name = 'indexer';

--
-- PostgreSQL database dump
--


-- Dumped from database version 16.13 (Debian 16.13-1.pgdg13+1)
-- Dumped by pg_dump version 16.13 (Debian 16.13-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: update_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.update_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id text NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    key_hash text NOT NULL,
    key_prefix text NOT NULL,
    key_hint text NOT NULL,
    workspace_id text NOT NULL,
    principal_id text NOT NULL,
    created_by text NOT NULL,
    on_behalf_of text DEFAULT ''::text NOT NULL,
    type text DEFAULT 'persistent'::text NOT NULL,
    environment text DEFAULT 'live'::text NOT NULL,
    permissions text[] DEFAULT '{}'::text[] NOT NULL,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    request_count bigint DEFAULT 0 NOT NULL,
    revoked boolean DEFAULT false NOT NULL,
    revoked_at timestamp with time zone,
    rotated_to_id text DEFAULT ''::text NOT NULL,
    grace_period_ends_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT api_keys_environment_check CHECK ((environment = ANY (ARRAY['live'::text, 'test'::text]))),
    CONSTRAINT api_keys_type_check CHECK ((type = ANY (ARRAY['persistent'::text, 'session'::text, 'restricted'::text])))
);


--
-- Name: auth_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_sessions (
    id text NOT NULL,
    workspace_id text NOT NULL,
    user_id text NOT NULL,
    session_hash text NOT NULL,
    provider text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT auth_sessions_provider_check CHECK ((provider = ANY (ARRAY['github'::text, 'google'::text])))
);


--
-- Name: bookmarks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bookmarks (
    id text NOT NULL,
    channel_id text NOT NULL,
    title text NOT NULL,
    type text DEFAULT 'link'::text NOT NULL,
    link text NOT NULL,
    emoji text DEFAULT ''::text NOT NULL,
    created_by text NOT NULL,
    updated_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: conversation_event_feed; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.conversation_event_feed (
    feed_id bigint NOT NULL,
    conversation_id text NOT NULL,
    external_event_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: conversation_event_feed_feed_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.conversation_event_feed_feed_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: conversation_event_feed_feed_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.conversation_event_feed_feed_id_seq OWNED BY public.conversation_event_feed.feed_id;


--
-- Name: conversation_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.conversation_members (
    conversation_id text NOT NULL,
    user_id text NOT NULL,
    joined_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: conversation_reads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.conversation_reads (
    workspace_id text NOT NULL,
    conversation_id text NOT NULL,
    user_id text NOT NULL,
    last_read_ts text NOT NULL,
    last_read_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: conversations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.conversations (
    id text NOT NULL,
    workspace_id text NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    type text NOT NULL,
    creator_id text NOT NULL,
    is_archived boolean DEFAULT false NOT NULL,
    topic_value text DEFAULT ''::text NOT NULL,
    topic_creator text DEFAULT ''::text NOT NULL,
    topic_last_set timestamp with time zone,
    purpose_value text DEFAULT ''::text NOT NULL,
    purpose_creator text DEFAULT ''::text NOT NULL,
    purpose_last_set timestamp with time zone,
    num_members integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT conversations_type_check CHECK ((type = ANY (ARRAY['public_channel'::text, 'private_channel'::text, 'im'::text, 'mpim'::text])))
);


--
-- Name: event_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.event_subscriptions (
    id text NOT NULL,
    workspace_id text NOT NULL,
    url text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    encrypted_secret text DEFAULT ''::text NOT NULL,
    event_type text DEFAULT ''::text NOT NULL,
    resource_type text DEFAULT ''::text NOT NULL,
    resource_id text DEFAULT ''::text NOT NULL,
    CONSTRAINT chk_event_subscriptions_resource_filter CHECK (((resource_id = ''::text) OR (resource_type <> ''::text)))
);


--
-- Name: external_event_projection_failures; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.external_event_projection_failures (
    id bigint NOT NULL,
    internal_event_id bigint NOT NULL,
    error text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: external_event_projection_failures_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.external_event_projection_failures_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: external_event_projection_failures_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.external_event_projection_failures_id_seq OWNED BY public.external_event_projection_failures.id;


--
-- Name: external_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.external_events (
    id bigint NOT NULL,
    workspace_id text NOT NULL,
    type text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    payload jsonb NOT NULL,
    source_internal_event_id bigint,
    source_internal_event_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    dedupe_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: external_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.external_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: external_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.external_events_id_seq OWNED BY public.external_events.id;


--
-- Name: file_channels; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.file_channels (
    file_id text NOT NULL,
    channel_id text NOT NULL,
    shared_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: file_event_feed; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.file_event_feed (
    feed_id bigint NOT NULL,
    file_id text NOT NULL,
    external_event_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: file_event_feed_feed_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.file_event_feed_feed_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: file_event_feed_feed_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.file_event_feed_feed_id_seq OWNED BY public.file_event_feed.feed_id;


--
-- Name: files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.files (
    id text NOT NULL,
    name text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    mimetype text DEFAULT ''::text NOT NULL,
    filetype text DEFAULT ''::text NOT NULL,
    size bigint DEFAULT 0 NOT NULL,
    user_id text NOT NULL,
    s3_key text DEFAULT ''::text NOT NULL,
    url_private text DEFAULT ''::text NOT NULL,
    url_private_download text DEFAULT ''::text NOT NULL,
    permalink text DEFAULT ''::text NOT NULL,
    is_external boolean DEFAULT false NOT NULL,
    external_url text DEFAULT ''::text NOT NULL,
    upload_complete boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    workspace_id text NOT NULL
);


--
-- Name: internal_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.internal_events (
    id bigint NOT NULL,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    workspace_id text DEFAULT ''::text NOT NULL,
    actor_id text DEFAULT ''::text NOT NULL,
    payload jsonb NOT NULL,
    metadata jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: internal_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.internal_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: internal_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.internal_events_id_seq OWNED BY public.internal_events.id;


--
-- Name: messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.messages (
    ts text NOT NULL,
    channel_id text NOT NULL,
    user_id text NOT NULL,
    text text DEFAULT ''::text NOT NULL,
    thread_ts text,
    type text DEFAULT 'message'::text NOT NULL,
    subtype text,
    blocks jsonb,
    metadata jsonb,
    edited_by text,
    edited_at text,
    reply_count integer DEFAULT 0 NOT NULL,
    reply_users_count integer DEFAULT 0 NOT NULL,
    latest_reply text,
    is_deleted boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: oauth_accounts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.oauth_accounts (
    id text NOT NULL,
    workspace_id text NOT NULL,
    user_id text NOT NULL,
    provider text NOT NULL,
    provider_subject text NOT NULL,
    email text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT oauth_accounts_provider_check CHECK ((provider = ANY (ARRAY['github'::text, 'google'::text])))
);


--
-- Name: pins; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pins (
    channel_id text NOT NULL,
    message_ts text NOT NULL,
    pinned_by text NOT NULL,
    pinned_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: projector_checkpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.projector_checkpoints (
    name text NOT NULL,
    last_event_id bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: reactions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.reactions (
    id bigint NOT NULL,
    channel_id text NOT NULL,
    message_ts text NOT NULL,
    user_id text NOT NULL,
    emoji text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: reactions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.reactions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: reactions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.reactions_id_seq OWNED BY public.reactions.id;


--
-- Name: workspace_event_feed; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workspace_event_feed (
    feed_id bigint NOT NULL,
    workspace_id text NOT NULL,
    external_event_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: workspace_event_feed_feed_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.workspace_event_feed_feed_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: workspace_event_feed_feed_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.workspace_event_feed_feed_id_seq OWNED BY public.workspace_event_feed.feed_id;


--
-- Name: user_event_feed; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_event_feed (
    feed_id bigint NOT NULL,
    user_id text NOT NULL,
    external_event_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: user_event_feed_feed_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.user_event_feed_feed_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: user_event_feed_feed_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.user_event_feed_feed_id_seq OWNED BY public.user_event_feed.feed_id;


--
-- Name: usergroup_event_feed; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usergroup_event_feed (
    feed_id bigint NOT NULL,
    usergroup_id text NOT NULL,
    external_event_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: usergroup_event_feed_feed_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.usergroup_event_feed_feed_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: usergroup_event_feed_feed_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.usergroup_event_feed_feed_id_seq OWNED BY public.usergroup_event_feed.feed_id;


--
-- Name: usergroup_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usergroup_members (
    usergroup_id text NOT NULL,
    user_id text NOT NULL,
    added_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: usergroups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usergroups (
    id text NOT NULL,
    workspace_id text NOT NULL,
    name text NOT NULL,
    handle text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    is_external boolean DEFAULT false NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    user_count integer DEFAULT 0 NOT NULL,
    created_by text NOT NULL,
    updated_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id text NOT NULL,
    workspace_id text NOT NULL,
    name text NOT NULL,
    real_name text DEFAULT ''::text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    email text DEFAULT ''::text NOT NULL,
    is_bot boolean DEFAULT false NOT NULL,
    account_type text DEFAULT ''::text NOT NULL,
    deleted boolean DEFAULT false NOT NULL,
    profile jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    principal_type text NOT NULL,
    owner_id text DEFAULT ''::text NOT NULL
);


--
-- Name: workspace_external_workspaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workspace_external_workspaces (
    id text NOT NULL,
    workspace_id text NOT NULL,
    external_workspace_id text NOT NULL,
    external_workspace_name text DEFAULT ''::text NOT NULL,
    connection_type text DEFAULT 'slack_connect'::text NOT NULL,
    connected boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    disconnected_at timestamp with time zone
);


--
-- Name: workspaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workspaces (
    id text NOT NULL,
    name text NOT NULL,
    domain text DEFAULT ''::text NOT NULL,
    email_domain text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    icon_image_original text DEFAULT ''::text NOT NULL,
    icon_image_34 text DEFAULT ''::text NOT NULL,
    icon_image_44 text DEFAULT ''::text NOT NULL,
    discoverability text DEFAULT 'invite_only'::text NOT NULL,
    default_channels text[] DEFAULT '{}'::text[] NOT NULL,
    preferences jsonb DEFAULT '{}'::jsonb NOT NULL,
    profile_fields jsonb DEFAULT '[]'::jsonb NOT NULL,
    billing_plan text DEFAULT 'free'::text NOT NULL,
    billing_status text DEFAULT 'active'::text NOT NULL,
    billing_email text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workspaces_discoverability_check CHECK ((discoverability = ANY (ARRAY['open'::text, 'invite_only'::text])))
);


--
-- Name: conversation_event_feed feed_id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_event_feed ALTER COLUMN feed_id SET DEFAULT nextval('public.conversation_event_feed_feed_id_seq'::regclass);


--
-- Name: external_event_projection_failures id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_event_projection_failures ALTER COLUMN id SET DEFAULT nextval('public.external_event_projection_failures_id_seq'::regclass);


--
-- Name: external_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_events ALTER COLUMN id SET DEFAULT nextval('public.external_events_id_seq'::regclass);


--
-- Name: file_event_feed feed_id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file_event_feed ALTER COLUMN feed_id SET DEFAULT nextval('public.file_event_feed_feed_id_seq'::regclass);


--
-- Name: internal_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.internal_events ALTER COLUMN id SET DEFAULT nextval('public.internal_events_id_seq'::regclass);


--
-- Name: reactions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reactions ALTER COLUMN id SET DEFAULT nextval('public.reactions_id_seq'::regclass);


--
-- Name: workspace_event_feed feed_id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspace_event_feed ALTER COLUMN feed_id SET DEFAULT nextval('public.workspace_event_feed_feed_id_seq'::regclass);


--
-- Name: user_event_feed feed_id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_event_feed ALTER COLUMN feed_id SET DEFAULT nextval('public.user_event_feed_feed_id_seq'::regclass);


--
-- Name: usergroup_event_feed feed_id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroup_event_feed ALTER COLUMN feed_id SET DEFAULT nextval('public.usergroup_event_feed_feed_id_seq'::regclass);


--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);


--
-- Name: auth_sessions auth_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_sessions
    ADD CONSTRAINT auth_sessions_pkey PRIMARY KEY (id);


--
-- Name: auth_sessions auth_sessions_session_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_sessions
    ADD CONSTRAINT auth_sessions_session_hash_key UNIQUE (session_hash);


--
-- Name: bookmarks bookmarks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bookmarks
    ADD CONSTRAINT bookmarks_pkey PRIMARY KEY (id);


--
-- Name: conversation_event_feed conversation_event_feed_conversation_id_external_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_event_feed
    ADD CONSTRAINT conversation_event_feed_conversation_id_external_event_id_key UNIQUE (conversation_id, external_event_id);


--
-- Name: conversation_event_feed conversation_event_feed_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_event_feed
    ADD CONSTRAINT conversation_event_feed_pkey PRIMARY KEY (feed_id);


--
-- Name: conversation_members conversation_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_members
    ADD CONSTRAINT conversation_members_pkey PRIMARY KEY (conversation_id, user_id);


--
-- Name: conversation_reads conversation_reads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_reads
    ADD CONSTRAINT conversation_reads_pkey PRIMARY KEY (conversation_id, user_id);


--
-- Name: conversations conversations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversations
    ADD CONSTRAINT conversations_pkey PRIMARY KEY (id);


--
-- Name: event_subscriptions event_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event_subscriptions
    ADD CONSTRAINT event_subscriptions_pkey PRIMARY KEY (id);


--
-- Name: external_event_projection_failures external_event_projection_failures_internal_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_event_projection_failures
    ADD CONSTRAINT external_event_projection_failures_internal_event_id_key UNIQUE (internal_event_id);


--
-- Name: external_event_projection_failures external_event_projection_failures_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_event_projection_failures
    ADD CONSTRAINT external_event_projection_failures_pkey PRIMARY KEY (id);


--
-- Name: external_events external_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_events
    ADD CONSTRAINT external_events_pkey PRIMARY KEY (id);


--
-- Name: external_events external_events_workspace_id_dedupe_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_events
    ADD CONSTRAINT external_events_workspace_id_dedupe_key_key UNIQUE (workspace_id, dedupe_key);


--
-- Name: file_channels file_channels_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file_channels
    ADD CONSTRAINT file_channels_pkey PRIMARY KEY (file_id, channel_id);


--
-- Name: file_event_feed file_event_feed_file_id_external_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file_event_feed
    ADD CONSTRAINT file_event_feed_file_id_external_event_id_key UNIQUE (file_id, external_event_id);


--
-- Name: file_event_feed file_event_feed_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file_event_feed
    ADD CONSTRAINT file_event_feed_pkey PRIMARY KEY (feed_id);


--
-- Name: files files_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_pkey PRIMARY KEY (id);


--
-- Name: internal_events internal_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.internal_events
    ADD CONSTRAINT internal_events_pkey PRIMARY KEY (id);


--
-- Name: messages messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (channel_id, ts);


--
-- Name: oauth_accounts oauth_accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_accounts
    ADD CONSTRAINT oauth_accounts_pkey PRIMARY KEY (id);


--
-- Name: pins pins_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pins
    ADD CONSTRAINT pins_pkey PRIMARY KEY (channel_id, message_ts);


--
-- Name: projector_checkpoints projector_checkpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projector_checkpoints
    ADD CONSTRAINT projector_checkpoints_pkey PRIMARY KEY (name);


--
-- Name: reactions reactions_channel_id_message_ts_user_id_emoji_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reactions
    ADD CONSTRAINT reactions_channel_id_message_ts_user_id_emoji_key UNIQUE (channel_id, message_ts, user_id, emoji);


--
-- Name: reactions reactions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reactions
    ADD CONSTRAINT reactions_pkey PRIMARY KEY (id);


--
-- Name: workspace_event_feed workspace_event_feed_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspace_event_feed
    ADD CONSTRAINT workspace_event_feed_pkey PRIMARY KEY (feed_id);


--
-- Name: workspace_event_feed workspace_event_feed_workspace_id_external_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspace_event_feed
    ADD CONSTRAINT workspace_event_feed_workspace_id_external_event_id_key UNIQUE (workspace_id, external_event_id);


--
-- Name: user_event_feed user_event_feed_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_event_feed
    ADD CONSTRAINT user_event_feed_pkey PRIMARY KEY (feed_id);


--
-- Name: user_event_feed user_event_feed_user_id_external_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_event_feed
    ADD CONSTRAINT user_event_feed_user_id_external_event_id_key UNIQUE (user_id, external_event_id);


--
-- Name: usergroup_event_feed usergroup_event_feed_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroup_event_feed
    ADD CONSTRAINT usergroup_event_feed_pkey PRIMARY KEY (feed_id);


--
-- Name: usergroup_event_feed usergroup_event_feed_usergroup_id_external_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroup_event_feed
    ADD CONSTRAINT usergroup_event_feed_usergroup_id_external_event_id_key UNIQUE (usergroup_id, external_event_id);


--
-- Name: usergroup_members usergroup_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroup_members
    ADD CONSTRAINT usergroup_members_pkey PRIMARY KEY (usergroup_id, user_id);


--
-- Name: usergroups usergroups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroups
    ADD CONSTRAINT usergroups_pkey PRIMARY KEY (id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: workspace_external_workspaces workspace_external_workspaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspace_external_workspaces
    ADD CONSTRAINT workspace_external_workspaces_pkey PRIMARY KEY (id);


--
-- Name: workspace_external_workspaces workspace_external_workspaces_workspace_id_external_workspace_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspace_external_workspaces
    ADD CONSTRAINT workspace_external_workspaces_workspace_id_external_workspace_id_key UNIQUE (workspace_id, external_workspace_id);


--
-- Name: workspaces workspaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_pkey PRIMARY KEY (id);


--
-- Name: idx_api_keys_created_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_created_by ON public.api_keys USING btree (created_by);


--
-- Name: idx_api_keys_key_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_key_hash ON public.api_keys USING btree (key_hash);


--
-- Name: idx_api_keys_principal_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_principal_id ON public.api_keys USING btree (principal_id);


--
-- Name: idx_api_keys_workspace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_workspace_id ON public.api_keys USING btree (workspace_id);


--
-- Name: idx_auth_sessions_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_sessions_user_id ON public.auth_sessions USING btree (user_id);


--
-- Name: idx_bookmarks_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bookmarks_channel_id ON public.bookmarks USING btree (channel_id);


--
-- Name: idx_conversation_event_feed_conversation_id_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_conversation_event_feed_conversation_id_external_event_id ON public.conversation_event_feed USING btree (conversation_id, external_event_id);


--
-- Name: idx_conversation_event_feed_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_conversation_event_feed_external_event_id ON public.conversation_event_feed USING btree (external_event_id);


--
-- Name: idx_conversation_members_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_conversation_members_user_id ON public.conversation_members USING btree (user_id);


--
-- Name: idx_conversation_reads_workspace_id_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_conversation_reads_workspace_id_user_id ON public.conversation_reads USING btree (workspace_id, user_id);


--
-- Name: idx_conversations_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_conversations_name ON public.conversations USING btree (name);


--
-- Name: idx_conversations_workspace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_conversations_workspace_id ON public.conversations USING btree (workspace_id);


--
-- Name: idx_conversations_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_conversations_type ON public.conversations USING btree (type);


--
-- Name: idx_event_subscriptions_workspace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_event_subscriptions_workspace_id ON public.event_subscriptions USING btree (workspace_id);


--
-- Name: idx_external_event_projection_failures_internal_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_external_event_projection_failures_internal_event_id ON public.external_event_projection_failures USING btree (internal_event_id);


--
-- Name: idx_external_events_source_internal_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_external_events_source_internal_event_id ON public.external_events USING btree (source_internal_event_id);


--
-- Name: idx_external_events_workspace_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_external_events_workspace_id_desc ON public.external_events USING btree (workspace_id, id DESC);


--
-- Name: idx_external_events_team_resource_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_external_events_team_resource_id_desc ON public.external_events USING btree (workspace_id, resource_type, resource_id, id DESC);


--
-- Name: idx_external_events_team_type_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_external_events_team_type_id_desc ON public.external_events USING btree (workspace_id, type, id DESC);


--
-- Name: idx_file_event_feed_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_file_event_feed_external_event_id ON public.file_event_feed USING btree (external_event_id);


--
-- Name: idx_file_event_feed_file_id_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_file_event_feed_file_id_external_event_id ON public.file_event_feed USING btree (file_id, external_event_id);


--
-- Name: idx_files_workspace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_files_workspace_id ON public.files USING btree (workspace_id);


--
-- Name: idx_files_workspace_id_file_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_files_workspace_id_file_id ON public.files USING btree (workspace_id, id);


--
-- Name: idx_files_workspace_id_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_files_workspace_id_user_id ON public.files USING btree (workspace_id, user_id);


--
-- Name: idx_files_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_files_user_id ON public.files USING btree (user_id);


--
-- Name: idx_internal_events_aggregate; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_internal_events_aggregate ON public.internal_events USING btree (aggregate_type, aggregate_id, id);


--
-- Name: idx_internal_events_team; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_internal_events_team ON public.internal_events USING btree (workspace_id, created_at DESC);


--
-- Name: idx_internal_events_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_internal_events_type ON public.internal_events USING btree (event_type, created_at DESC);


--
-- Name: idx_messages_channel_ts; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_messages_channel_ts ON public.messages USING btree (channel_id, ts DESC);


--
-- Name: idx_messages_thread; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_messages_thread ON public.messages USING btree (channel_id, thread_ts) WHERE (thread_ts IS NOT NULL);


--
-- Name: idx_messages_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_messages_user_id ON public.messages USING btree (user_id);


--
-- Name: idx_oauth_accounts_identity; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_oauth_accounts_identity ON public.oauth_accounts USING btree (workspace_id, provider, provider_subject);


--
-- Name: idx_oauth_accounts_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_oauth_accounts_user_id ON public.oauth_accounts USING btree (user_id);


--
-- Name: idx_reactions_message; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_reactions_message ON public.reactions USING btree (channel_id, message_ts);


--
-- Name: idx_workspace_event_feed_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workspace_event_feed_external_event_id ON public.workspace_event_feed USING btree (external_event_id);


--
-- Name: idx_workspace_event_feed_workspace_id_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workspace_event_feed_workspace_id_external_event_id ON public.workspace_event_feed USING btree (workspace_id, external_event_id);


--
-- Name: idx_user_event_feed_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_event_feed_external_event_id ON public.user_event_feed USING btree (external_event_id);


--
-- Name: idx_user_event_feed_user_id_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_event_feed_user_id_external_event_id ON public.user_event_feed USING btree (user_id, external_event_id);


--
-- Name: idx_usergroup_event_feed_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usergroup_event_feed_external_event_id ON public.usergroup_event_feed USING btree (external_event_id);


--
-- Name: idx_usergroup_event_feed_usergroup_id_external_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usergroup_event_feed_usergroup_id_external_event_id ON public.usergroup_event_feed USING btree (usergroup_id, external_event_id);


--
-- Name: idx_usergroups_team_handle; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_usergroups_team_handle ON public.usergroups USING btree (workspace_id, handle);


--
-- Name: idx_usergroups_workspace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usergroups_workspace_id ON public.usergroups USING btree (workspace_id);


--
-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_email ON public.users USING btree (email) WHERE (email <> ''::text);


--
-- Name: idx_users_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_name ON public.users USING btree (name);


--
-- Name: idx_users_owner_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_owner_id ON public.users USING btree (owner_id) WHERE (owner_id <> ''::text);


--
-- Name: idx_users_principal_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_principal_type ON public.users USING btree (principal_type);


--
-- Name: idx_users_team_email; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_team_email ON public.users USING btree (workspace_id, email) WHERE (email <> ''::text);


--
-- Name: idx_users_workspace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_workspace_id ON public.users USING btree (workspace_id);


--
-- Name: idx_workspace_external_workspaces_workspace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workspace_external_workspaces_workspace_id ON public.workspace_external_workspaces USING btree (workspace_id);


--
-- Name: idx_workspaces_domain; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workspaces_domain ON public.workspaces USING btree (domain);


--
-- Name: idx_workspaces_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workspaces_name ON public.workspaces USING btree (name);


--
-- Name: api_keys trg_api_keys_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_api_keys_updated_at BEFORE UPDATE ON public.api_keys FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: bookmarks trg_bookmarks_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_bookmarks_updated_at BEFORE UPDATE ON public.bookmarks FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: conversations trg_conversations_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_conversations_updated_at BEFORE UPDATE ON public.conversations FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: event_subscriptions trg_event_subscriptions_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_event_subscriptions_updated_at BEFORE UPDATE ON public.event_subscriptions FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: files trg_files_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_files_updated_at BEFORE UPDATE ON public.files FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: messages trg_messages_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_messages_updated_at BEFORE UPDATE ON public.messages FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: oauth_accounts trg_oauth_accounts_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_oauth_accounts_updated_at BEFORE UPDATE ON public.oauth_accounts FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: usergroups trg_usergroups_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_usergroups_updated_at BEFORE UPDATE ON public.usergroups FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: users trg_users_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: workspaces trg_workspaces_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_workspaces_updated_at BEFORE UPDATE ON public.workspaces FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: api_keys api_keys_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: api_keys api_keys_principal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.users(id);


--
-- Name: auth_sessions auth_sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_sessions
    ADD CONSTRAINT auth_sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: bookmarks bookmarks_channel_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bookmarks
    ADD CONSTRAINT bookmarks_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.conversations(id) ON DELETE CASCADE;


--
-- Name: bookmarks bookmarks_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bookmarks
    ADD CONSTRAINT bookmarks_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: conversation_event_feed conversation_event_feed_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_event_feed
    ADD CONSTRAINT conversation_event_feed_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;


--
-- Name: conversation_event_feed conversation_event_feed_external_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_event_feed
    ADD CONSTRAINT conversation_event_feed_external_event_id_fkey FOREIGN KEY (external_event_id) REFERENCES public.external_events(id) ON DELETE CASCADE;


--
-- Name: conversation_members conversation_members_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_members
    ADD CONSTRAINT conversation_members_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;


--
-- Name: conversation_members conversation_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_members
    ADD CONSTRAINT conversation_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: conversations conversations_creator_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversations
    ADD CONSTRAINT conversations_creator_id_fkey FOREIGN KEY (creator_id) REFERENCES public.users(id);


--
-- Name: external_event_projection_failures external_event_projection_failures_internal_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_event_projection_failures
    ADD CONSTRAINT external_event_projection_failures_internal_event_id_fkey FOREIGN KEY (internal_event_id) REFERENCES public.internal_events(id) ON DELETE CASCADE;


--
-- Name: external_events external_events_source_internal_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.external_events
    ADD CONSTRAINT external_events_source_internal_event_id_fkey FOREIGN KEY (source_internal_event_id) REFERENCES public.internal_events(id) ON DELETE RESTRICT;


--
-- Name: file_channels file_channels_channel_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file_channels
    ADD CONSTRAINT file_channels_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.conversations(id) ON DELETE CASCADE;


--
-- Name: file_channels file_channels_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file_channels
    ADD CONSTRAINT file_channels_file_id_fkey FOREIGN KEY (file_id) REFERENCES public.files(id) ON DELETE CASCADE;


--
-- Name: file_event_feed file_event_feed_external_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file_event_feed
    ADD CONSTRAINT file_event_feed_external_event_id_fkey FOREIGN KEY (external_event_id) REFERENCES public.external_events(id) ON DELETE CASCADE;


--
-- Name: file_event_feed file_event_feed_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.file_event_feed
    ADD CONSTRAINT file_event_feed_file_id_fkey FOREIGN KEY (file_id) REFERENCES public.files(id) ON DELETE CASCADE;


--
-- Name: files files_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);


--
-- Name: messages messages_channel_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.conversations(id) ON DELETE CASCADE;


--
-- Name: messages messages_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);


--
-- Name: oauth_accounts oauth_accounts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.oauth_accounts
    ADD CONSTRAINT oauth_accounts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: pins pins_channel_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pins
    ADD CONSTRAINT pins_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.conversations(id) ON DELETE CASCADE;


--
-- Name: pins pins_channel_id_message_ts_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pins
    ADD CONSTRAINT pins_channel_id_message_ts_fkey FOREIGN KEY (channel_id, message_ts) REFERENCES public.messages(channel_id, ts) ON DELETE CASCADE;


--
-- Name: pins pins_pinned_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pins
    ADD CONSTRAINT pins_pinned_by_fkey FOREIGN KEY (pinned_by) REFERENCES public.users(id);


--
-- Name: reactions reactions_channel_id_message_ts_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reactions
    ADD CONSTRAINT reactions_channel_id_message_ts_fkey FOREIGN KEY (channel_id, message_ts) REFERENCES public.messages(channel_id, ts) ON DELETE CASCADE;


--
-- Name: reactions reactions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reactions
    ADD CONSTRAINT reactions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);


--
-- Name: workspace_event_feed workspace_event_feed_external_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspace_event_feed
    ADD CONSTRAINT workspace_event_feed_external_event_id_fkey FOREIGN KEY (external_event_id) REFERENCES public.external_events(id) ON DELETE CASCADE;


--
-- Name: user_event_feed user_event_feed_external_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_event_feed
    ADD CONSTRAINT user_event_feed_external_event_id_fkey FOREIGN KEY (external_event_id) REFERENCES public.external_events(id) ON DELETE CASCADE;


--
-- Name: user_event_feed user_event_feed_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_event_feed
    ADD CONSTRAINT user_event_feed_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: usergroup_event_feed usergroup_event_feed_external_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroup_event_feed
    ADD CONSTRAINT usergroup_event_feed_external_event_id_fkey FOREIGN KEY (external_event_id) REFERENCES public.external_events(id) ON DELETE CASCADE;


--
-- Name: usergroup_event_feed usergroup_event_feed_usergroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroup_event_feed
    ADD CONSTRAINT usergroup_event_feed_usergroup_id_fkey FOREIGN KEY (usergroup_id) REFERENCES public.usergroups(id) ON DELETE CASCADE;


--
-- Name: usergroup_members usergroup_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroup_members
    ADD CONSTRAINT usergroup_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: usergroup_members usergroup_members_usergroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroup_members
    ADD CONSTRAINT usergroup_members_usergroup_id_fkey FOREIGN KEY (usergroup_id) REFERENCES public.usergroups(id) ON DELETE CASCADE;


--
-- Name: usergroups usergroups_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usergroups
    ADD CONSTRAINT usergroups_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: workspace_external_workspaces workspace_external_workspaces_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspace_external_workspaces
    ADD CONSTRAINT workspace_external_workspaces_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;


CREATE TABLE IF NOT EXISTS public.user_role_assignments (
    id text NOT NULL,
    workspace_id text NOT NULL,
    user_id text NOT NULL,
    role_key text NOT NULL,
    assigned_by text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_role_assignments_unique
    ON public.user_role_assignments USING btree (workspace_id, user_id, role_key);

CREATE INDEX IF NOT EXISTS idx_user_role_assignments_user
    ON public.user_role_assignments USING btree (workspace_id, user_id);

ALTER TABLE ONLY public.user_role_assignments
    ADD CONSTRAINT user_role_assignments_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.user_role_assignments
    ADD CONSTRAINT user_role_assignments_assigned_by_fkey FOREIGN KEY (assigned_by) REFERENCES public.users(id);


CREATE TABLE IF NOT EXISTS public.conversation_manager_assignments (
    conversation_id text NOT NULL,
    user_id text NOT NULL,
    assigned_by text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_manager_assignments_unique
    ON public.conversation_manager_assignments USING btree (conversation_id, user_id);

CREATE INDEX IF NOT EXISTS idx_conversation_manager_assignments_user
    ON public.conversation_manager_assignments USING btree (user_id);

ALTER TABLE ONLY public.conversation_manager_assignments
    ADD CONSTRAINT conversation_manager_assignments_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.conversation_manager_assignments
    ADD CONSTRAINT conversation_manager_assignments_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.conversation_manager_assignments
    ADD CONSTRAINT conversation_manager_assignments_assigned_by_fkey FOREIGN KEY (assigned_by) REFERENCES public.users(id);


CREATE TABLE IF NOT EXISTS public.conversation_posting_policies (
    conversation_id text NOT NULL,
    policy_type text NOT NULL,
    policy_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_by text NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.conversation_posting_policies
    ADD CONSTRAINT conversation_posting_policies_pkey PRIMARY KEY (conversation_id);

ALTER TABLE ONLY public.conversation_posting_policies
    ADD CONSTRAINT conversation_posting_policies_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.conversation_posting_policies
    ADD CONSTRAINT conversation_posting_policies_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id);


CREATE TABLE IF NOT EXISTS public.external_principal_access (
    id text NOT NULL,
    host_workspace_id text NOT NULL,
    principal_id text NOT NULL,
    principal_type text NOT NULL,
    home_workspace_id text NOT NULL,
    access_mode text NOT NULL,
    allowed_capabilities jsonb DEFAULT '[]'::jsonb NOT NULL,
    granted_by text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    expires_at timestamptz,
    revoked_at timestamptz
);

ALTER TABLE ONLY public.external_principal_access
    ADD CONSTRAINT external_principal_access_pkey PRIMARY KEY (id);

CREATE INDEX IF NOT EXISTS idx_external_principal_access_principal
    ON public.external_principal_access USING btree (host_workspace_id, principal_id);

ALTER TABLE ONLY public.external_principal_access
    ADD CONSTRAINT external_principal_access_host_workspace_id_fkey FOREIGN KEY (host_workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_principal_access
    ADD CONSTRAINT external_principal_access_principal_id_fkey FOREIGN KEY (principal_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_principal_access
    ADD CONSTRAINT external_principal_access_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES public.users(id);


CREATE TABLE IF NOT EXISTS public.external_principal_conversation_assignments (
    access_id text NOT NULL,
    conversation_id text NOT NULL,
    granted_by text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_principal_conversation_assignments_unique
    ON public.external_principal_conversation_assignments USING btree (access_id, conversation_id);

CREATE INDEX IF NOT EXISTS idx_external_principal_conversation_assignments_conversation
    ON public.external_principal_conversation_assignments USING btree (conversation_id);

ALTER TABLE ONLY public.external_principal_conversation_assignments
    ADD CONSTRAINT external_principal_conversation_assignments_access_id_fkey FOREIGN KEY (access_id) REFERENCES public.external_principal_access(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_principal_conversation_assignments
    ADD CONSTRAINT external_principal_conversation_assignments_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.external_principal_conversation_assignments
    ADD CONSTRAINT external_principal_conversation_assignments_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES public.users(id);


CREATE TABLE IF NOT EXISTS public.authorization_audit_log (
    id text NOT NULL,
    workspace_id text NOT NULL,
    actor_id text,
    api_key_id text,
    on_behalf_of text,
    action text NOT NULL,
    resource text NOT NULL,
    resource_id text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.authorization_audit_log
    ADD CONSTRAINT authorization_audit_log_pkey PRIMARY KEY (id);

CREATE INDEX IF NOT EXISTS idx_authorization_audit_log_team_created
    ON public.authorization_audit_log USING btree (workspace_id, created_at DESC, id DESC);

ALTER TABLE ONLY public.authorization_audit_log
    ADD CONSTRAINT authorization_audit_log_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.authorization_audit_log
    ADD CONSTRAINT authorization_audit_log_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.authorization_audit_log
    ADD CONSTRAINT authorization_audit_log_on_behalf_of_fkey FOREIGN KEY (on_behalf_of) REFERENCES public.users(id) ON DELETE SET NULL;

UPDATE public.users
SET account_type = 'member'
WHERE principal_type = 'human' AND account_type = '';

ALTER TABLE public.users
    ADD CONSTRAINT users_account_type_by_principal_check CHECK (
        (principal_type = 'human' AND account_type = ANY (ARRAY['primary_admin'::text, 'admin'::text, 'member'::text])) OR
        (principal_type <> 'human' AND account_type = ''::text)
    );

CREATE TABLE public.workspace_invites (
    id text NOT NULL,
    workspace_id text NOT NULL,
    email text NOT NULL,
    invited_by text NOT NULL,
    token_hash text NOT NULL,
    accepted_by_user_id text,
    expires_at timestamp with time zone NOT NULL,
    accepted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_token_hash_key UNIQUE (token_hash);

CREATE INDEX idx_workspace_invites_workspace_id ON public.workspace_invites USING btree (workspace_id);
CREATE INDEX idx_workspace_invites_email ON public.workspace_invites USING btree (LOWER(email));

CREATE TRIGGER trg_workspace_invites_updated_at BEFORE UPDATE ON public.workspace_invites
    FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_accepted_by_user_id_fkey FOREIGN KEY (accepted_by_user_id) REFERENCES public.users(id);

DROP INDEX IF EXISTS public.idx_workspaces_domain;

CREATE UNIQUE INDEX idx_workspaces_domain
    ON public.workspaces (domain);

ALTER TABLE public.api_keys ALTER COLUMN principal_id DROP NOT NULL;
ALTER TABLE public.api_keys ALTER COLUMN principal_id SET DEFAULT '';
ALTER TABLE public.api_keys ALTER COLUMN principal_id DROP DEFAULT;
UPDATE public.api_keys SET principal_id = NULL WHERE principal_id = '';

ALTER TABLE public.conversations
ADD COLUMN last_message_ts text,
ADD COLUMN last_activity_ts text;

CREATE TABLE public.canonical_dms (
    workspace_id text NOT NULL,
    user_low_id text NOT NULL,
    user_high_id text NOT NULL,
    conversation_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (workspace_id, user_low_id, user_high_id),
    UNIQUE (conversation_id)
);

CREATE TABLE public.thread_participants (
    channel_id text NOT NULL,
    thread_ts text NOT NULL,
    user_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (channel_id, thread_ts, user_id)
);

ALTER TABLE public.canonical_dms
    ADD CONSTRAINT canonical_dms_conversation_id_fkey
    FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;

ALTER TABLE public.canonical_dms
    ADD CONSTRAINT canonical_dms_user_low_id_fkey
    FOREIGN KEY (user_low_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE public.canonical_dms
    ADD CONSTRAINT canonical_dms_user_high_id_fkey
    FOREIGN KEY (user_high_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE public.thread_participants
    ADD CONSTRAINT thread_participants_channel_id_thread_ts_fkey
    FOREIGN KEY (channel_id, thread_ts) REFERENCES public.messages(channel_id, ts) ON DELETE CASCADE;

ALTER TABLE public.thread_participants
    ADD CONSTRAINT thread_participants_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE INDEX idx_conversations_last_activity_ts
ON public.conversations (last_activity_ts DESC NULLS LAST, id DESC);

CREATE INDEX idx_conversation_members_user_conversation
ON public.conversation_members (user_id, conversation_id);

CREATE INDEX idx_thread_participants_thread
ON public.thread_participants (channel_id, thread_ts);

UPDATE public.conversations c
SET last_message_ts = latest.ts
FROM (
    SELECT channel_id, MAX(ts) AS ts
    FROM public.messages
    WHERE thread_ts IS NULL AND is_deleted = FALSE
    GROUP BY channel_id
) AS latest
WHERE c.id = latest.channel_id;

UPDATE public.conversations c
SET last_activity_ts = latest.ts
FROM (
    SELECT channel_id, MAX(ts) AS ts
    FROM public.messages
    WHERE is_deleted = FALSE
    GROUP BY channel_id
) AS latest
WHERE c.id = latest.channel_id;

INSERT INTO public.thread_participants (channel_id, thread_ts, user_id)
SELECT DISTINCT m.channel_id, m.thread_ts, m.user_id
FROM public.messages m
WHERE m.thread_ts IS NOT NULL AND m.is_deleted = FALSE;

INSERT INTO public.canonical_dms (workspace_id, user_low_id, user_high_id, conversation_id)
SELECT
    c.workspace_id,
    members.user_low_id,
    members.user_high_id,
    c.id
FROM public.conversations c
JOIN (
    SELECT
        cm.conversation_id,
        MIN(cm.user_id) AS user_low_id,
        MAX(cm.user_id) AS user_high_id
    FROM public.conversation_members cm
    GROUP BY cm.conversation_id
    HAVING COUNT(*) = 2
) AS members
    ON members.conversation_id = c.id
WHERE c.type = 'im';

ALTER TABLE public.internal_events
ADD COLUMN shard_key text NOT NULL DEFAULT '',
ADD COLUMN shard_id integer NOT NULL DEFAULT 0;

UPDATE public.internal_events
SET shard_key = CASE
        WHEN aggregate_type = 'conversation' THEN aggregate_id
        WHEN workspace_id <> '' THEN workspace_id
        ELSE aggregate_id
    END;

UPDATE public.internal_events
SET shard_id = ((hashtext(shard_key)::bigint & 2147483647) % 16)::integer;

CREATE INDEX idx_internal_events_shard_id_id
ON public.internal_events (shard_id, id);

CREATE INDEX idx_internal_events_shard_key_id
ON public.internal_events (shard_key, id);

CREATE TABLE public.oauth_authorization_codes (
    id text PRIMARY KEY,
    code_hash text NOT NULL UNIQUE,
    client_id text NOT NULL,
    client_name text NOT NULL,
    redirect_uri text NOT NULL,
    workspace_id text NOT NULL REFERENCES public.workspaces(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    scope text[] NOT NULL DEFAULT '{}',
    resource text NOT NULL,
    code_challenge text NOT NULL,
    code_challenge_method text NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT oauth_authorization_codes_code_challenge_method_check CHECK (code_challenge_method = 'S256')
);

CREATE INDEX idx_oauth_authorization_codes_user_id ON public.oauth_authorization_codes(user_id);
CREATE INDEX idx_oauth_authorization_codes_workspace_id ON public.oauth_authorization_codes(workspace_id);
CREATE INDEX idx_oauth_authorization_codes_expires_at ON public.oauth_authorization_codes(expires_at);

CREATE TRIGGER trg_oauth_authorization_codes_updated_at
BEFORE UPDATE ON public.oauth_authorization_codes
FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();

CREATE TABLE public.oauth_refresh_tokens (
    id text PRIMARY KEY,
    token_hash text NOT NULL UNIQUE,
    client_id text NOT NULL,
    client_name text NOT NULL,
    workspace_id text NOT NULL REFERENCES public.workspaces(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    scope text[] NOT NULL DEFAULT '{}',
    resource text NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    rotated_to_id text REFERENCES public.oauth_refresh_tokens(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_oauth_refresh_tokens_user_id ON public.oauth_refresh_tokens(user_id);
CREATE INDEX idx_oauth_refresh_tokens_workspace_id ON public.oauth_refresh_tokens(workspace_id);
CREATE INDEX idx_oauth_refresh_tokens_expires_at ON public.oauth_refresh_tokens(expires_at);

CREATE TRIGGER trg_oauth_refresh_tokens_updated_at
BEFORE UPDATE ON public.oauth_refresh_tokens
FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- PostgreSQL database dump complete
--

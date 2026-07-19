CREATE TABLE schema_version (
    version integer not null
);

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username text not null unique,
    password text,
    is_admin int default 0,
    language text default 'en_US',
    timezone text default 'UTC',
    theme text default 'light_serif',
    last_login_at DATETIME,
    keyboard_shortcuts int default 1,
    entries_per_page int default 100,
    show_reading_time int default 1,
    entry_swipe int default 1,
    entry_direction text default 'asc' CHECK(entry_direction IN ('asc', 'desc')),
    entry_order text default 'published_at' CHECK(entry_order IN ('published_at', 'created_at')),
    gesture_nav text default 'tap',
    default_reading_speed int default 265,
    cjk_reading_speed int default 500,
    default_home_page text default 'unread',
    categories_sorting_order text not null default 'unread_count',
    mark_read_on_view int default 1,
    stylesheet text not null default '',
    google_id text not null default '',
    openid_connect_id text not null default '',
    display_mode text default 'standalone' CHECK(display_mode IN ('fullscreen', 'standalone', 'minimal-ui', 'browser')),
    media_playback_rate real default 1,
    block_filter_entry_rules text not null default '',
    keep_filter_entry_rules text not null default '',
    mark_read_on_media_player_completion int default 0,
    custom_js text not null default '',
    external_font_hosts text not null default '',
    always_open_external_links int default 0,
    open_external_links_in_new_tab int default 1
);

CREATE UNIQUE INDEX users_google_id_idx ON users(google_id) WHERE google_id <> '';
CREATE UNIQUE INDEX users_openid_connect_id_idx ON users(openid_connect_id) WHERE openid_connect_id <> '';

CREATE TABLE categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id int not null,
    title text not null,
    hide_globally int not null default 0,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE (user_id, title)
);

CREATE TABLE feeds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id int not null,
    category_id int not null,
    title text not null,
    feed_url text not null,
    site_url text not null,
    checked_at DATETIME default (datetime('now')),
    etag_header text default '',
    last_modified_header text default '',
    parsing_error_msg text default '',
    parsing_error_count int default 0,
    scraper_rules text default '',
    rewrite_rules text default '',
    crawler int default 0,
    username text default '',
    password text default '',
    user_agent text default '',
    disabled int default 0,
    next_check_at DATETIME default (datetime('now')),
    ignore_http_cache int default 0,
    fetch_via_proxy int default 0,
    blocklist_rules text not null default '',
    keeplist_rules text not null default '',
    allow_self_signed_certificates int not null default 0,
    cookie text default '',
    hide_globally int not null default 0,
    description text default '',
    disable_http2 int default 0,
    url_rewrite_rules text not null default '',
    no_media_player int default 0,
    apprise_service_urls text default '',
    ntfy_enabled int default 0,
    ntfy_priority int default 3,
    webhook_url text default '',
    pushover_enabled int default 0,
    pushover_priority int default 0,
    ntfy_topic text default '',
    proxy_url text default '',
    block_filter_entry_rules text not null default '',
    keep_filter_entry_rules text not null default '',
    ignore_entry_updates int default 0,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE,
    UNIQUE (user_id, feed_url)
);

CREATE INDEX feeds_user_category_idx ON feeds(user_id, category_id);

CREATE TABLE entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id int not null,
    feed_id bigint not null,
    hash text not null,
    published_at DATETIME not null,
    title text not null,
    url text not null,
    author text,
    content text,
    status text default 'unread' CHECK(status IN ('unread', 'read', 'removed')),
    starred int default 0,
    comments_url text default '',
    changed_at DATETIME not null,
    share_code text not null default '',
    reading_time int not null default 0,
    created_at DATETIME not null default (datetime('now')),
    document_vectors text not null default '',
    tags text default '[]',
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE,
    UNIQUE (feed_id, hash)
);

CREATE INDEX entries_feed_idx ON entries(feed_id);
CREATE INDEX entries_user_status_idx ON entries(user_id, status);
CREATE INDEX entries_user_feed_idx ON entries(user_id, feed_id);
CREATE INDEX entries_id_user_status_idx ON entries(id, user_id, status);
CREATE INDEX entries_feed_id_status_hash_idx ON entries(feed_id, status, hash);
CREATE INDEX entries_user_id_status_starred_idx ON entries(user_id, status, starred);
CREATE INDEX entries_user_status_feed_idx ON entries(user_id, status, feed_id);
CREATE INDEX entries_user_status_changed_idx ON entries(user_id, status, changed_at);
CREATE UNIQUE INDEX entries_share_code_idx ON entries(share_code) WHERE share_code <> '';

CREATE VIRTUAL TABLE fts_entries USING fts5(
    title,
    content,
    content='entries',
    content_rowid='rowid',
    tokenize='porter unicode61'
);

CREATE TRIGGER entries_fts_ai AFTER INSERT ON entries BEGIN
    INSERT INTO fts_entries(rowid, title, content)
    VALUES (new.rowid, new.title, new.content);
END;

CREATE TRIGGER entries_fts_ad AFTER DELETE ON entries BEGIN
    INSERT INTO fts_entries(fts_entries, rowid, title, content)
    VALUES ('delete', old.rowid, old.title, old.content);
END;

CREATE TRIGGER entries_fts_au AFTER UPDATE ON entries BEGIN
    INSERT INTO fts_entries(fts_entries, rowid, title, content)
    VALUES ('delete', old.rowid, old.title, old.content);
    INSERT INTO fts_entries(rowid, title, content)
    VALUES (new.rowid, new.title, new.content);
END;

CREATE TABLE enclosures (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id int not null,
    entry_id bigint not null,
    url text not null,
    size bigint default 0,
    mime_type text default '',
    media_progression bigint default 0,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX enclosures_user_entry_md5url_unique ON enclosures(user_id, entry_id, md5(url));

CREATE TABLE icons (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hash text not null unique,
    mime_type text not null,
    content BLOB not null,
    external_id text default ''
);

CREATE UNIQUE INDEX icons_external_id_idx ON icons(external_id) WHERE external_id <> '';

CREATE TABLE feed_icons (
    feed_id bigint not null,
    icon_id bigint not null,
    PRIMARY KEY(feed_id, icon_id),
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE,
    FOREIGN KEY (icon_id) REFERENCES icons(id) ON DELETE CASCADE
);

CREATE TABLE sessions (
    id text not null,
    data text not null,
    created_at DATETIME not null default (datetime('now')),
    PRIMARY KEY(id)
);

CREATE TABLE integrations (
    user_id int not null,
    pinboard_enabled int default 0,
    pinboard_token text default '',
    pinboard_tags text default 'miniflux',
    pinboard_mark_as_unread int default 0,
    instapaper_enabled int default 0,
    instapaper_username text default '',
    instapaper_password text default '',
    fever_enabled int default 0,
    fever_username text default '',
    fever_token text default '',
    googlereader_enabled int default 0,
    googlereader_username text default '',
    googlereader_password text default '',
    wallabag_enabled int default 0,
    wallabag_url text default '',
    wallabag_client_id text default '',
    wallabag_client_secret text default '',
    wallabag_username text default '',
    wallabag_password text default '',
    wallabag_tags text default '',
    wallabag_only_url int default 0,
    nunux_keeper_enabled int default 0,
    nunux_keeper_url text default '',
    nunux_keeper_api_key text default '',
    pocket_enabled int default 0,
    pocket_access_token text default '',
    pocket_consumer_key text default '',
    telegram_bot_enabled int default 0,
    telegram_bot_token text default '',
    telegram_bot_chat_id text default '',
    telegram_bot_topic_id int,
    telegram_bot_disable_web_page_preview int default 0,
    telegram_bot_disable_notification int default 0,
    telegram_bot_disable_buttons int default 0,
    espial_enabled int default 0,
    espial_url text default '',
    espial_api_key text default '',
    espial_tags text default 'miniflux',
    linkding_enabled int default 0,
    linkding_url text default '',
    linkding_api_key text default '',
    linkding_tags text default '',
    linkding_mark_as_unread int default 0,
    rssbridge_enabled int default 0,
    rssbridge_url text default '',
    rssbridge_token text default '',
    omnivore_enabled int default 0,
    omnivore_api_key text default '',
    omnivore_url text default '',
    linkace_enabled int default 0,
    linkace_url text default '',
    linkace_api_key text default '',
    linkace_tags text default '',
    linkace_is_private int default 1,
    linkace_check_disabled int default 1,
    linkwarden_enabled int default 0,
    linkwarden_url text default '',
    linkwarden_api_key text default '',
    linkwarden_collection_id int,
    readeck_enabled int default 0,
    readeck_url text default '',
    readeck_api_key text default '',
    readeck_labels text default '',
    readeck_only_url int default 0,
    readeck_push_enabled int default 0,
    raindrop_enabled int default 0,
    raindrop_token text default '',
    raindrop_collection_id text default '',
    raindrop_tags text default '',
    betula_enabled int default 0,
    betula_url text default '',
    betula_token text default '',
    ntfy_enabled int default 0,
    ntfy_topic text default '',
    ntfy_url text default '',
    ntfy_api_token text default '',
    ntfy_username text default '',
    ntfy_password text default '',
    ntfy_icon_url text default '',
    ntfy_internal_links int default 0,
    cubox_enabled int default 0,
    cubox_api_link text default '',
    discord_enabled int default 0,
    discord_webhook_link text default '',
    slack_enabled int default 0,
    slack_webhook_link text default '',
    pushover_enabled int default 0,
    pushover_user text default '',
    pushover_token text default '',
    pushover_device text default '',
    pushover_prefix text default '',
    karakeep_enabled int default 0,
    karakeep_api_key text default '',
    karakeep_url text default '',
    karakeep_tags text default '',
    linktaco_enabled int default 0,
    linktaco_api_token text default '',
    linktaco_org_slug text default '',
    linktaco_tags text default '',
    linktaco_visibility text default 'PUBLIC' CHECK(linktaco_visibility IN ('PUBLIC', 'PRIVATE')),
    archiveorg_enabled int default 0,
    notion_enabled int default 0,
    notion_token text default '',
    notion_page_id text default '',
    readwise_enabled int default 0,
    readwise_api_key text default '',
    apprise_enabled int default 0,
    apprise_url text default '',
    apprise_services_url text default '',
    shiori_enabled int default 0,
    shiori_url text default '',
    shiori_username text default '',
    shiori_password text default '',
    shaarli_enabled int default 0,
    shaarli_url text default '',
    shaarli_api_secret text default '',
    webhook_enabled int default 0,
    webhook_url text default '',
    webhook_secret text default '',
    matrix_bot_enabled int default 0,
    matrix_bot_user text default '',
    matrix_bot_password text default '',
    matrix_bot_url text default '',
    matrix_bot_chat_id text default '',
    PRIMARY KEY(user_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id int not null REFERENCES users(id) ON DELETE CASCADE,
    token text not null unique,
    description text not null,
    last_used_at DATETIME,
    created_at DATETIME default (datetime('now')),
    UNIQUE (user_id, description)
);

CREATE TABLE acme_cache (
    key varchar(400) not null primary key,
    data BLOB not null,
    updated_at DATETIME not null
);

CREATE TABLE webauthn_credentials (
    handle BLOB primary key,
    cred_id BLOB unique not null,
    user_id int REFERENCES users(id) ON DELETE CASCADE not null,
    public_key BLOB not null,
    attestation_type varchar(255) not null,
    aaguid BLOB,
    sign_count bigint,
    clone_warning int,
    name text not null default '',
    added_on DATETIME default (datetime('now')),
    last_seen_on DATETIME default (datetime('now'))
);

CREATE TABLE web_sessions (
    id text not null,
    secret_hash BLOB not null,
    user_id int REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME not null default (datetime('now')),
    user_agent text not null default '',
    ip text,
    state text not null default '{}',
    PRIMARY KEY (id),
    CHECK (json_type(state) = 'object')
);

CREATE INDEX web_sessions_user_id_idx ON web_sessions(user_id) WHERE user_id IS NOT NULL;
CREATE INDEX web_sessions_created_at_idx ON web_sessions(created_at);

CREATE TABLE entry_tombstones (
    feed_id bigint not null REFERENCES feeds(id) ON DELETE CASCADE,
    hash text not null CHECK (hash <> ''),
    deleted_at DATETIME not null default (datetime('now')),
    PRIMARY KEY (feed_id, hash)
);

CREATE INDEX entry_tombstones_deleted_at_idx ON entry_tombstones(deleted_at);

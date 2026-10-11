--
-- PostgreSQL database dump, cut down for tests
--

CREATE TABLE public.aicg_chronicles (
    id_hash text NOT NULL
);

COPY public.aicg_chronicles (id_hash, text, url, "timestamp") FROM stdin;
h1	a log	https://example.com	2024-01-01 00:00:00+00
\.

COPY public.booru_character_def (author, name, id, definition, raw, comments, tags, image_hash, tagline, summary, metadata, created, added, source, hidden) FROM stdin;
cat	Cora	+pygmalion1	{"data": {"name": "Cora", "description": "A booru card.", "first_mes": "Hello.", "creator": "cat", "tags": ["oc"]}, "spec": "chara_card_v2", "spec_version": "2.0"}	\\x00	[]	{oc,female}	0	\N	\N	{}	2023-06-05 01:36:32.965+00	2024-05-08 05:10:30.593534+00	booru	f
\.

COPY public.chub_character (author, name, id, data, chats, ratings, updated, added, source, metadata, hidden) FROM stdin;
ann	Aria	100	{"id": 100, "rating": 4.5, "topics": ["Fantasy", "Elf"]}	{"chats": []}	{"ratings": []}	2025-01-02 17:46:38.876621+00	2024-05-06 17:36:35.697631+00	chub	{}	f
\.

COPY public.chub_character_def (author, name, id, full_path, definition, raw, image_hash, original_card_hash, added, source, metadata, hidden, increment) FROM stdin;
ann	Aria	100	ann/aria	{"data": {"name": "Aria", "description": "An elf ranger.", "first_mes": "Hello.", "creator": "ann", "tags": ["ranger"]}, "spec": "chara_card_v2", "spec_version": "2.0"}	\\x00	abc0000000000000000000000000000d	0	2024-05-06 20:00:28.368094+00	chub	{}	f	5
bob	Brin	101	bob/brin	{"data": {"name": "Brin", "description": "A dwarf smith.", "first_mes": "Hello.", "creator": "bob", "tags": []}, "spec": "chara_card_v2", "spec_version": "2.0"}	\\x00	fff0000000000000000000000000000f	0	2024-06-01 00:00:00+00	chub	{}	t	7
x	Bad	102	x/bad	{"data": "not a card"}	\\x00	0	0	2024-06-01 00:00:00+00	chub	{}	f	8
ann	Aria	100	ann/aria	{"data": {"name": "Aria", "description": "An elf ranger who says \\"hi\\".\\nShe guards\\tthe woods.", "first_mes": "Hello.", "creator": "ann", "tags": ["ranger"]}, "spec": "chara_card_v2", "spec_version": "2.0"}	\\x00	abc0000000000000000000000000000d	0	2024-10-02 09:50:16.236022+00	chub	{}	f	9
\.

COPY public.chub_lorebook (author, name, id, data, ratings, updated, added, source, hidden) FROM stdin;
ann	Eldoria	200	{"id": 200, "rating": 3, "topics": ["World"]}	{"ratings": []}	2025-01-02 08:33:31.690431+00	2024-05-07 02:16:18.124364+00	chub	f
\.

COPY public.chub_lorebook_def (author, name, id, full_path, definition, raw, image_hash, added, metadata, source, increment) FROM stdin;
ann	Eldoria	200	lorebooks/ann/eldoria	{"name": "Eldoria", "entries": [{"id": 1, "keys": ["Eldoria"], "content": "A kingdom of elves.", "enabled": true, "insertion_order": 0, "extensions": {}}], "extensions": {}}	\\x00	0	2024-05-07 02:16:18.124005+00	{}	chub	3
\.

COPY public.chub_user (username, id, data, image_hash, updated, added, source) FROM stdin;
ann	1	{}	0	2025-01-02 00:00:00+00	2024-05-06 00:00:00+00	chub
\.

COPY public.generic_character_def (card_data_hash, name, definition, raw, image_hash, summary, tagline, added, metadata, source, source_url, hidden, increment) FROM stdin;
3cfc7793db330003fc1ff39895edc8cc	Vergil	{"data": {"name": "Vergil", "description": "A half-demon swordsman.", "first_mes": "Hello.", "creator": "", "tags": []}, "spec": "chara_card_v2", "spec_version": "2.0"}	\\x00	0	\N	\N	2024-05-08 15:38:35.643506+00	{}	janitorai	\N	f	65394
\.

COPY public.nyaime_character_def (author, name, id, definition, raw, image_hash, original_card_hash, metadata, added, source, increment) FROM stdin;
dee	Ava	43	{"data": {"name": "Ava", "description": "A nyaime card.", "first_mes": "Hello.", "creator": "dee", "tags": []}, "spec": "chara_card_v2", "spec_version": "2.0"}	\\x00	0	0	{}	2024-10-05 07:27:31.990945+00	nyaime	4
\.

COPY public.risuai_character_def (author, name, id, definition, raw, image_hash, original_card_hash, metadata, added, source, increment) FROM stdin;
eve	Nina	0c95bece-1d02-416f-8834-7a87895af295	{"data": {"name": "Nina", "description": "A risu card.", "first_mes": "Hello.", "creator": "eve", "tags": []}, "spec": "chara_card_v2", "spec_version": "2.0"}	\\x00	0	0	{}	2024-10-15 06:20:48.072827+00	risuai	2633
\.

COPY public.stored_rentry_links (url, added) FROM stdin;
https://rentry.org/x	2024-01-01 00:00:00+00
\.

COPY public.webring_character_def (card_data_hash, name, author, definition, raw, image_hash, tagline, summary, added, metadata, source, source_url, hidden, increment) FROM stdin;
cdef8582d2d146742a302972a605b19b	Esther	crustcrunch.neocities.org	{"data": {"name": "Esther", "description": "A vampire knight.", "first_mes": "Hello.", "creator": "", "tags": []}, "spec": "chara_card_v2", "spec_version": "2.0"}	\\x00	0	\N	\N	2024-11-27 02:03:44.637213+00	{}	webring	\N	f	112
\.

--
-- PostgreSQL database dump complete
--

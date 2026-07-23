import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const {
	isNotFoundError,
	projectRow,
	projectShow,
	shouldResolveNotFound,
	singleResolveCandidate,
	splitTagExpressions,
} = require('../dist/nodes/Kura/Kura.node.js');

assert.deepEqual(
	projectRow({
		metadataRef: 'tvdb:370070',
		title: 'Bookworm',
		canonicalTitle: 'Ascendance of a Bookworm',
		status: 'incomplete',
		isAiring: true,
		staged: false,
		episodesAvailable: 12,
		episodeCount: 14,
		posterUrl: 'https://example.invalid/poster.jpg',
		lastScanned: '2026-04-20T03:00:00Z',
		searchKey: 'bookworm',
		tags: ['priority'],
	}),
	{
		metadataRef: 'tvdb:370070',
		title: 'Bookworm',
		canonicalTitle: 'Ascendance of a Bookworm',
		status: 'incomplete',
		isAiring: true,
		staged: false,
		tags: ['priority'],
	},
);

assert.deepEqual(splitTagExpressions(' priority   !maintenance-disabled '), [
	'priority',
	'!maintenance-disabled',
]);
assert.deepEqual(splitTagExpressions('  '), []);

const fullRow = {
	metadataRef: 'tvdb:370070',
	title: 'Bookworm',
	status: 'incomplete',
	episodesAvailable: 12,
	episodeCount: 14,
	posterUrl: 'https://example.invalid/poster.jpg',
	lastScanned: '2026-04-20T03:00:00Z',
};
assert.equal(projectRow(fullRow, false), fullRow);

assert.deepEqual(
	projectShow({
		metadataRef: 'tvdb:370070',
		ref: 'Bookworm',
		root: 'library:Bookworm',
		lastScanned: '2026-04-20T03:00:00Z',
		preferredTitle: 'Bookworm',
		canonicalTitle: 'Ascendance of a Bookworm',
		status: 'incomplete',
		isAiring: false,
		tags: ['mute-notifications'],
		artwork: { poster: { url: 'https://example.invalid/poster.jpg' } },
		seasons: [
			{
				number: 0,
				summary: { episodeCount: 1, present: 1 },
				episodes: [{ episode: 'S00E0001', status: 'present' }],
			},
			{
				number: 1,
				summary: { episodeCount: 2, present: 1, missing: 1 },
				episodes: [
					{
						episode: 'S01E0001',
						aired: '2019-10-03',
						status: 'present',
						preferredTitle: 'Episode One',
						canonicalTitle: 'Episode One Canonical',
						active: {
							source: 'WebRip',
							resolution: '1080p',
							codec: 'HEVC',
							attrs: {
								origin: 'takuhai',
								release_group: 'SubsPlease',
							},
							size: 123,
							file: 'series:Season 1/Bookworm - S01E01.mkv',
							companions: [
								{
									path: 'series:Season 1/Bookworm - S01E01.en.srt',
									role: 'subtitle',
									language: 'en',
									label: 'English',
									size: 10,
									mtime: '2026-04-20T03:00:00Z',
								},
							],
						},
						staged: {
							source: 'BDRip',
							resolution: '1920x1080',
							codec: 'AVC',
							attrs: {
								origin: 'manual',
							},
							size: 456,
							file: 'inbox:Bookworm S01E01.mkv',
							companions: [],
						},
					},
				],
			},
		],
		stagedTrash: [{ id: 'trash' }],
		stagedExtras: [{ id: 'extra' }],
	}),
	{
		metadataRef: 'tvdb:370070',
		preferredTitle: 'Bookworm',
		canonicalTitle: 'Ascendance of a Bookworm',
		status: 'incomplete',
		isAiring: false,
		tags: ['mute-notifications'],
		seasons: [
			{
				number: 1,
				episodes: [
					{
						episode: 'S01E0001',
						aired: '2019-10-03',
						status: 'present',
						activeMediaFile: {
							resolution: '1080p',
							source: 'WebRip',
							codec: 'HEVC',
							attrs: {
								origin: 'takuhai',
								release_group: 'SubsPlease',
							},
							hasSubtitles: true,
						},
						stagedMediaFile: {
							resolution: '1920x1080',
							source: 'BDRip',
							codec: 'AVC',
							attrs: {
								origin: 'manual',
							},
							hasSubtitles: false,
						},
					},
				],
			},
		],
	},
);

assert.deepEqual(
	projectShow(
		{
			metadataRef: 'tvdb:370070',
			ref: 'Bookworm',
			root: 'library:Bookworm',
			artwork: { poster: { url: 'https://example.invalid/poster.jpg' } },
			seasons: [
				{ number: 0, episodes: [{ episode: 'S00E0001', status: 'present' }] },
				{ number: 1, summary: { episodeCount: 1 }, episodes: [] },
			],
			stagedTrash: [{ id: 'trash' }],
		},
		false,
		false,
	),
	{
		metadataRef: 'tvdb:370070',
		ref: 'Bookworm',
		root: 'library:Bookworm',
		artwork: { poster: { url: 'https://example.invalid/poster.jpg' } },
		tags: [],
		seasons: [{ number: 1, summary: { episodeCount: 1 }, episodes: [] }],
		stagedTrash: [{ id: 'trash' }],
	},
);

assert.deepEqual(
	projectShow(
		{
			metadataRef: 'tvdb:370070',
			preferredTitle: 'Bookworm',
			status: 'complete',
			seasons: [
				{
					number: 0,
					episodes: [{ episode: 'S00E0001', status: 'present' }],
				},
			],
		},
		true,
	),
	{
		metadataRef: 'tvdb:370070',
		preferredTitle: 'Bookworm',
		status: 'complete',
		isAiring: false,
		tags: [],
		seasons: [
			{
				number: 0,
				episodes: [{ episode: 'S00E0001', status: 'present' }],
			},
		],
	},
);

assert.equal(isNotFoundError({ httpCode: '404' }), true);
assert.equal(isNotFoundError({ status: 404 }), true);
assert.equal(isNotFoundError({ statusCode: 404 }), true);
assert.equal(isNotFoundError({ response: { status: 404 } }), true);
assert.equal(isNotFoundError({ response: { statusCode: 404 } }), true);
assert.equal(isNotFoundError({ httpCode: '500' }), false);
assert.equal(isNotFoundError(new Error('not found')), false);
assert.equal(shouldResolveNotFound(false, { httpCode: '404' }), true);
assert.equal(shouldResolveNotFound(true, { httpCode: '404' }), false);
assert.equal(shouldResolveNotFound(false, { httpCode: '500' }), false);
assert.deepEqual(
	singleResolveCandidate(
		{
			candidates: [
				{
					ref: 'tvdb:370070',
					preferredTitle: 'Bookworm',
				},
			],
		},
		'tvdb:370070',
	),
	{
		ref: 'tvdb:370070',
		preferredTitle: 'Bookworm',
	},
);
assert.throws(
	() => singleResolveCandidate({ candidates: [] }, 'tvdb:404'),
	/resolve returned 0 candidates for metadata ref tvdb:404/,
);

console.log('projection-check ok');

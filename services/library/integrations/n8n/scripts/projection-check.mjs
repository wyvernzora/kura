import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const { projectRow, projectShow } = require('../dist/nodes/Kura/Kura.node.js');

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
	}),
	{
		metadataRef: 'tvdb:370070',
		title: 'Bookworm',
		canonicalTitle: 'Ascendance of a Bookworm',
		status: 'incomplete',
		isAiring: true,
		staged: false,
	},
);

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
							hasSubtitles: true,
						},
						stagedMediaFile: {
							resolution: '1920x1080',
							source: 'BDRip',
							codec: 'AVC',
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
		seasons: [
			{
				number: 0,
				episodes: [{ episode: 'S00E0001', status: 'present' }],
			},
		],
	},
);

console.log('projection-check ok');

import type {
	IDataObject,
	IExecuteFunctions,
	IHttpRequestMethods,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
} from 'n8n-workflow';

const CRED = 'kuraApi';
const PAGE_LIMIT = 1000;
const ACTIONABLE_STATUSES = new Set(['complete', 'incomplete']);

export class Kura implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'Kura',
		name: 'kura',
		group: ['transform'],
		version: 1,
		subtitle: '={{$parameter["operation"] + " (" + $parameter["resource"] + ")"}}',
		description: 'Read Kura library state for anime automation workflows',
		defaults: { name: 'Kura' },
		inputs: ['main'],
		outputs: ['main'],
		credentials: [{ name: CRED, required: true }],
		properties: [
			{
				displayName: 'Resource',
				name: 'resource',
				type: 'options',
				noDataExpression: true,
				options: [{ name: 'Series', value: 'series' }],
				default: 'series',
			},
			{
				displayName: 'Operation',
				name: 'operation',
				type: 'options',
				noDataExpression: true,
				displayOptions: { show: { resource: ['series'] } },
				options: [
					{ name: 'List', value: 'list', action: 'List actionable series' },
					{ name: 'Show', value: 'show', action: 'Show series state' },
				],
				default: 'list',
			},
			{
				displayName: 'Statuses',
				name: 'statuses',
				type: 'multiOptions',
				options: [
					{ name: 'Complete', value: 'complete' },
					{ name: 'Incomplete', value: 'incomplete' },
				],
				default: ['incomplete'],
				description: 'Series statuses to include. Error and untracked rows are always skipped.',
				displayOptions: { show: { resource: ['series'], operation: ['list'] } },
			},
			{
				displayName: 'Simplify Output',
				name: 'simplifyOutput',
				type: 'boolean',
				default: true,
				description: 'Whether to return an agent-focused projection instead of the native Kura REST shape',
				displayOptions: { show: { resource: ['series'] } },
			},
			{
				displayName: 'Airing',
				name: 'airing',
				type: 'options',
				options: [
					{ name: 'Any', value: 'any' },
					{ name: 'Airing', value: 'airing' },
					{ name: 'Not Airing', value: 'notAiring' },
				],
				default: 'any',
				description: 'Filter by Kura observed-airing flag',
				displayOptions: { show: { resource: ['series'], operation: ['list'] } },
			},
			{
				displayName: 'Metadata Ref',
				name: 'metadataRef',
				type: 'string',
				default: '={{$json.metadataRef}}',
				required: true,
				description: 'Kura metadata ref, for example tvdb:370070',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Episodes',
				name: 'episodes',
				type: 'string',
				default: '',
				placeholder: 'ALL, NONE, AIRING_SEASON, S1, or S1E1-12',
				description: 'Optional Kura episode selector. Empty means ALL.',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Episode Status',
				name: 'status',
				type: 'multiOptions',
				options: [
					{ name: 'Pending', value: 'pending' },
					{ name: 'Missing', value: 'missing' },
					{ name: 'Present', value: 'present' },
					{ name: 'Staged', value: 'staged' },
					{ name: 'Staged Replacement', value: 'staged_replacement' },
				],
				default: [],
				description: 'Optional episode statuses to include',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Include Specials',
				name: 'includeSpecials',
				type: 'boolean',
				default: false,
				description: 'Whether to include season 0 specials in the output',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Source',
				name: 'source',
				type: 'string',
				default: '',
				placeholder: 'WebRip,BluRay',
				description: 'Optional comma-separated active-media sources',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Resolution',
				name: 'resolution',
				type: 'string',
				default: '',
				placeholder: '1080p,4K',
				description: 'Optional comma-separated active-media resolutions',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
		],
	};

	async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
		const operation = this.getNodeParameter('operation', 0) as string;
		const credentials = await this.getCredentials(CRED);
		const call = callFactory(this, credentials);

		if (operation === 'list') {
			const statuses = this.getNodeParameter('statuses', 0) as string[];
			const airing = this.getNodeParameter('airing', 0) as string;
			const simplifyOutput = this.getNodeParameter('simplifyOutput', 0) as boolean;
			const rows = await listAll(call, statuses, airing);
			return [
				rows
					.filter((row) => ACTIONABLE_STATUSES.has(stringField(row, 'status')))
					.filter((row) => stringField(row, 'metadataRef') !== '')
					.map((row) => ({ json: projectRow(row, simplifyOutput) })),
			];
		}

		const items = this.getInputData();
		const out: INodeExecutionData[] = [];
		for (let i = 0; i < items.length; i++) {
			try {
				const metadataRef = this.getNodeParameter('metadataRef', i) as string;
				const includeSpecials = this.getNodeParameter('includeSpecials', i) as boolean;
				const simplifyOutput = this.getNodeParameter('simplifyOutput', i) as boolean;
				const result = await call('GET', showPath(metadataRef, queryFor(this, i)));
				out.push({ json: projectShow(result, includeSpecials, simplifyOutput), pairedItem: { item: i } });
			} catch (error) {
				if (this.continueOnFail()) {
					out.push({ json: { error: (error as Error).message }, pairedItem: { item: i } });
					continue;
				}
				throw error;
			}
		}
		return [out];
	}
}

type HTTPCall = (method: IHttpRequestMethods, path: string) => Promise<IDataObject>;

function callFactory(ctx: IExecuteFunctions, credentials: IDataObject): HTTPCall {
	const baseUrl = String(credentials.baseUrl).replace(/\/+$/, '');
	const bearerToken = String(credentials.bearerToken ?? '');
	const headers = bearerToken === '' ? undefined : { Authorization: `Bearer ${bearerToken}` };
	return (method, path) =>
		ctx.helpers.httpRequest({
			method,
			url: `${baseUrl}${path}`,
			headers,
			json: true,
		}) as Promise<IDataObject>;
}

async function listAll(call: HTTPCall, statuses: string[], airing: string): Promise<IDataObject[]> {
	const rows: IDataObject[] = [];
	let cursor = '';
	do {
		const query = new URLSearchParams();
		const effectiveStatuses = statuses.length === 0 ? ['complete', 'incomplete'] : statuses;
		for (const status of effectiveStatuses) query.append('status', status);
		if (airing === 'airing') query.set('airing', 'true');
		if (airing === 'notAiring') query.set('airing', 'false');
		query.set('limit', String(PAGE_LIMIT));
		if (cursor !== '') query.set('cursor', cursor);

		const result = await call('GET', `/api/v1/series?${query.toString()}`);
		rows.push(...arrayField(result, 'rows'));
		cursor = stringField(result, 'nextCursor');
	} while (cursor !== '');
	return rows;
}

export function projectRow(row: IDataObject, simplifyOutput = true): IDataObject {
	if (!simplifyOutput) return row;
	return dropEmpty({
		metadataRef: row.metadataRef,
		title: row.title,
		canonicalTitle: row.canonicalTitle,
		status: row.status,
		isAiring: Boolean(row.isAiring),
		staged: Boolean(row.staged),
	});
}

function queryFor(ctx: IExecuteFunctions, itemIndex: number): URLSearchParams {
	const query = new URLSearchParams();
	const episodes = ctx.getNodeParameter('episodes', itemIndex) as string;
	if (episodes !== '') query.set('episodes', episodes);
	for (const status of ctx.getNodeParameter('status', itemIndex) as string[]) {
		query.append('status', status);
	}
	for (const source of csv(ctx.getNodeParameter('source', itemIndex) as string)) {
		query.append('source', source);
	}
	for (const resolution of csv(ctx.getNodeParameter('resolution', itemIndex) as string)) {
		query.append('resolution', resolution);
	}
	return query;
}

function showPath(metadataRef: string, query: URLSearchParams): string {
	const suffix = query.toString();
	const path = `/api/v1/series/${encodeURIComponent(metadataRef)}`;
	return suffix === '' ? path : `${path}?${suffix}`;
}

function csv(value: string): string[] {
	return value
		.split(',')
		.map((part) => part.trim())
		.filter((part) => part !== '');
}

export function projectShow(
	show: IDataObject,
	includeSpecials = false,
	simplifyOutput = true,
): IDataObject {
	if (!simplifyOutput) return withSpecialsFilter(show, includeSpecials);

	const out = dropEmpty({
		metadataRef: show.metadataRef,
		preferredTitle: show.preferredTitle,
		canonicalTitle: show.canonicalTitle,
		status: show.status,
		isAiring: Boolean(show.isAiring),
	});
	out.seasons = filteredSeasons(show, includeSpecials).map(projectSeason);
	return out;
}

function withSpecialsFilter(show: IDataObject, includeSpecials: boolean): IDataObject {
	if (includeSpecials) return show;
	return {
		...show,
		seasons: filteredSeasons(show, false),
	};
}

function filteredSeasons(show: IDataObject, includeSpecials: boolean): IDataObject[] {
	return arrayField(show, 'seasons').filter((season) => includeSpecials || season.number !== 0);
}

function projectSeason(season: IDataObject): IDataObject {
	return dropEmpty({
		number: season.number,
		episodes: arrayField(season, 'episodes').map(projectEpisode),
	});
}

function projectEpisode(episode: IDataObject): IDataObject {
	return dropEmpty({
		episode: episode.episode,
		aired: episode.aired,
		status: episode.status,
		activeMediaFile: projectMediaFile(objectField(episode, 'active')),
		stagedMediaFile: projectMediaFile(objectField(episode, 'staged')),
	});
}

function projectMediaFile(media: IDataObject | undefined): IDataObject | undefined {
	if (!media) return undefined;
	return dropEmpty({
		resolution: media.resolution,
		source: media.source,
		codec: media.codec,
		attrs: media.attrs,
		hasSubtitles: hasSubtitles(media),
	});
}

function hasSubtitles(media: IDataObject): boolean {
	return arrayField(media, 'companions').some((companion) => {
		const role = stringField(companion, 'role').toLowerCase();
		const path = stringField(companion, 'path').toLowerCase();
		return (
			role === 'subtitle' ||
			path.endsWith('.ass') ||
			path.endsWith('.srt') ||
			path.endsWith('.ssa') ||
			path.endsWith('.vtt')
		);
	});
}

function arrayField(obj: IDataObject, key: string): IDataObject[] {
	const value = obj[key];
	return Array.isArray(value) ? (value.filter(isObject) as IDataObject[]) : [];
}

function stringField(obj: IDataObject, key: string): string {
	const value = obj[key];
	return typeof value === 'string' ? value : '';
}

function objectField(obj: IDataObject, key: string): IDataObject | undefined {
	const value = obj[key];
	return isObject(value) ? value : undefined;
}

function isObject(value: unknown): value is IDataObject {
	return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function dropEmpty(obj: IDataObject): IDataObject {
	const out: IDataObject = {};
	for (const [key, value] of Object.entries(obj)) {
		if (value === undefined || value === null || value === '') continue;
		if (Array.isArray(value) && value.length === 0) continue;
		out[key] = value as IDataObject[string];
	}
	return out;
}

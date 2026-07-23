import type {
	IDataObject,
	IExecuteFunctions,
	IHttpRequestMethods,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
	JsonObject,
} from 'n8n-workflow';
import { LoggerProxy as Logger } from 'n8n-workflow';

const LIBRARY_CRED = 'kuraLibraryApi';
const RELEASES_CRED = 'kuraReleasesApi';
const PAGE_LIMIT = 1000;
const ACTIONABLE_STATUSES = new Set(['complete', 'incomplete']);

export class Kura implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'Kura',
		name: 'kura',
		group: ['transform'],
		version: 1,
		icon: 'file:kura.svg',
		subtitle: '={{$parameter["operation"] + " (" + $parameter["resource"] + ")"}}',
		description:
			'Interact with the Kura suite: library series, release ingest, and the matcher queue',
		documentationUrl: 'https://github.com/wyvernzora/kura/tree/main/integrations/n8n',
		codex: {
			categories: ['Data & Storage'],
			resources: {
				primaryDocumentation: [
					{
						url: 'https://github.com/wyvernzora/kura/tree/main/integrations/n8n',
					},
				],
			},
			alias: ['Anime library', 'Plex library', 'Media library', 'Release indexer'],
		},
		defaults: { name: 'Kura' },
		inputs: ['main'],
		outputs:
			'={{$parameter["operation"] === "show" && $parameter["errorOnNotFound"] === false ? ["main", "main"] : ["main"]}}',
		outputNames: ['tracked', 'untracked'],
		credentials: [
			{
				name: LIBRARY_CRED,
				required: true,
				displayOptions: { show: { resource: ['series'] } },
			},
			{
				name: RELEASES_CRED,
				required: true,
				displayOptions: { show: { resource: ['release', 'queue'] } },
			},
		],
		properties: [
			{
				displayName: 'Resource',
				name: 'resource',
				type: 'options',
				noDataExpression: true,
				options: [
					{ name: 'Series', value: 'series' },
					{ name: 'Release', value: 'release' },
					{ name: 'Queue', value: 'queue' },
				],
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
					{ name: 'Update Tags', value: 'updateTags', action: 'Update tags on a series' },
				],
				default: 'list',
			},
			{
				displayName: 'Operation',
				name: 'operation',
				type: 'options',
				noDataExpression: true,
				displayOptions: { show: { resource: ['release'] } },
				options: [
					{ name: 'Ingest', value: 'ingest', action: 'Ingest releases' },
					{ name: 'Get', value: 'get', action: 'Get a release' },
					{
						name: 'Get Magnet Link',
						value: 'getMagnetLink',
						action: 'Get a release magnet link',
					},
				],
				default: 'ingest',
			},
			{
				displayName: 'Operation',
				name: 'operation',
				type: 'options',
				noDataExpression: true,
				displayOptions: { show: { resource: ['queue'] } },
				options: [
					{ name: 'Claim', value: 'claim', action: 'Claim a batch of releases' },
					{ name: 'Submit Dispositions', value: 'submit', action: 'Submit disposition results' },
					{ name: 'Get Queue Stats', value: 'queueStats', action: 'Read the queue counts' },
				],
				default: 'claim',
			},

			// ----- series (library-manager) -----
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
				displayOptions: { show: { resource: ['series'], operation: ['list', 'show'] } },
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
				displayName: 'Tags',
				name: 'tags',
				type: 'string',
				default: '',
				placeholder: 'priority:high !maintenance:disabled',
				description: 'Space-delimited tag filter. Plain tags must be present; !tag expressions must be absent.',
				displayOptions: { show: { resource: ['series'], operation: ['list'] } },
			},
			{
				displayName: 'Metadata Ref',
				name: 'metadataRef',
				type: 'string',
				default: '={{$json.metadataRef}}',
				required: true,
				description: 'Kura metadata ref, for example tvdb:370070',
				displayOptions: { show: { resource: ['series'], operation: ['show', 'updateTags'] } },
			},
			{
				displayName: 'Tag Changes',
				name: 'tagChanges',
				type: 'string',
				default: '',
				required: true,
				placeholder: 'maintenance:requested !maintenance:disabled',
				description: 'Space-delimited changes. Plain tags add; !tag expressions remove.',
				displayOptions: { show: { resource: ['series'], operation: ['updateTags'] } },
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
			{
				displayName: 'Error on Not Found',
				name: 'errorOnNotFound',
				type: 'boolean',
				default: true,
				description: 'Whether to throw an error when Kura has no series for the metadata ref',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},

			// ----- release (release-indexer) -----
			{
				displayName: 'Posts',
				name: 'posts',
				type: 'json',
				default: '={{ $json.posts }}',
				required: true,
				description: 'The raw posts payload, forwarded as-is to /ingest',
				displayOptions: { show: { resource: ['release'], operation: ['ingest'] } },
			},
			{
				displayName: 'Infohash',
				name: 'infohash',
				type: 'string',
				default: '={{ $json.infohash }}',
				required: true,
				displayOptions: { show: { resource: ['release'], operation: ['get', 'getMagnetLink'] } },
			},

			// ----- queue (release-indexer) -----
			{
				displayName: 'Limit',
				name: 'limit',
				type: 'number',
				typeOptions: { minValue: 1 },
				default: 10,
				description: 'Max releases to claim',
				displayOptions: { show: { resource: ['queue'], operation: ['claim'] } },
			},
			{
				displayName: 'Lease Seconds',
				name: 'lease_seconds',
				type: 'number',
				default: 300,
				description: 'Lease length; honored if supplied, else a server default',
				displayOptions: { show: { resource: ['queue'], operation: ['claim'] } },
			},
			{
				displayName: 'Body',
				name: 'body',
				type: 'json',
				default: '={{ $json }}',
				required: true,
				description:
					'A single /submit body, an array of /submit bodies, an object with items, or a structured-output object with output.items',
				displayOptions: { show: { resource: ['queue'], operation: ['submit'] } },
			},
		],
	};

	async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
		const resource = this.getNodeParameter('resource', 0) as string;
		if (resource === 'series') {
			return executeSeries.call(this);
		}
		return executeIndexer.call(this, resource);
	}
}

// ----- series resource: the library-manager REST API -----

async function executeSeries(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
	const operation = this.getNodeParameter('operation', 0) as string;
	const credentials = await this.getCredentials(LIBRARY_CRED);
	const call = callFactory(this, credentials);

	if (operation === 'list') {
		const statuses = this.getNodeParameter('statuses', 0) as string[];
		const airing = this.getNodeParameter('airing', 0) as string;
		const simplifyOutput = this.getNodeParameter('simplifyOutput', 0) as boolean;
		const tags = this.getNodeParameter('tags', 0) as string;
		const rows = await listAll(call, statuses, airing, tags);
		return [
			rows
				.filter((row) => ACTIONABLE_STATUSES.has(stringField(row, 'status')))
				.filter((row) => stringField(row, 'metadataRef') !== '')
				.map((row) => ({ json: projectRow(row, simplifyOutput) })),
		];
	}

	if (operation === 'updateTags') {
		const items = this.getInputData();
		const out: INodeExecutionData[] = [];
		for (let i = 0; i < items.length; i++) {
			const metadataRef = this.getNodeParameter('metadataRef', i) as string;
			const tags = splitTagExpressions(this.getNodeParameter('tagChanges', i) as string);
			try {
				if (tags.length === 0) throw new Error('at least one tag expression is required');
				const result = await call(
					'PATCH',
					`/api/v1/series/${encodeURIComponent(metadataRef)}/tags`,
					{ tags },
				);
				out.push({ json: result, pairedItem: { item: i } });
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

	const items = this.getInputData();
	const out: INodeExecutionData[] = [];
	const resolvedNotFound: INodeExecutionData[] = [];
	let emitUntrackedOutput = false;
	for (let i = 0; i < items.length; i++) {
		const errorOnNotFound = shouldErrorOnNotFound(this, i);
		if (!errorOnNotFound) emitUntrackedOutput = true;
		const metadataRef = this.getNodeParameter('metadataRef', i) as string;
		try {
			const includeSpecials = this.getNodeParameter('includeSpecials', i) as boolean;
			const simplifyOutput = this.getNodeParameter('simplifyOutput', i) as boolean;
			const result = await call('GET', showPath(metadataRef, queryFor(this, i)));
			out.push({ json: projectShow(result, includeSpecials, simplifyOutput), pairedItem: { item: i } });
		} catch (error) {
			if (shouldResolveNotFound(errorOnNotFound, error)) {
				const result = await call('POST', '/api/v1/resolve', { terms: [metadataRef] });
				resolvedNotFound.push({
					json: singleResolveCandidate(result, metadataRef),
					pairedItem: { item: i },
				});
				continue;
			}
			if (this.continueOnFail()) {
				out.push({ json: { error: (error as Error).message }, pairedItem: { item: i } });
				continue;
			}
			throw error;
		}
	}
	return emitUntrackedOutput ? [out, resolvedNotFound] : [out];
}

// ----- release + queue resources: the release-indexer REST API -----

async function executeIndexer(
	this: IExecuteFunctions,
	resource: string,
): Promise<INodeExecutionData[][]> {
	const items = this.getInputData();
	const operation = this.getNodeParameter('operation', 0) as string;

	const credentials = await this.getCredentials(RELEASES_CRED);
	const baseUrl = String(credentials.baseUrl).replace(/\/+$/, '');
	Logger.info('Kura indexer execution started', { resource, operation, item_count: items.length });

	const call = (method: IHttpRequestMethods, path: string, body?: IDataObject) =>
		this.helpers.httpRequest({
			method,
			url: `${baseUrl}${path}`,
			body,
			json: true,
		}) as Promise<IDataObject>;

	if (operation === 'queueStats') {
		const stats = await call('GET', '/queue/stats');
		Logger.info('Kura queue stats fetched', {
			available: stats.available,
			leased: stats.leased,
			exhausted: stats.exhausted,
		});
		return [[{ json: stats }]];
	}

	if (operation === 'claim') {
		const res = await call('POST', '/queue/claim', {
			limit: this.getNodeParameter('limit', 0),
			lease_seconds: this.getNodeParameter('lease_seconds', 0),
		});
		const claimed = (res.items as IDataObject[]) ?? [];
		Logger.info('Kura queue claim completed', { claimed_count: claimed.length });
		return [[{ json: { items: claimed, count: claimed.length } }]];
	}

	const out: INodeExecutionData[] = [];
	for (let i = 0; i < items.length; i++) {
		try {
			if (operation === 'ingest') {
				const posts = this.getNodeParameter('posts', i);
				const res = await call('POST', '/ingest', { posts });
				Logger.info('Kura ingest completed', {
					item_index: i,
					post_count: Array.isArray(posts) ? posts.length : undefined,
					new_count: (res.batch as IDataObject | undefined)?.new,
					updated_count: (res.batch as IDataObject | undefined)?.updated,
					duplicate_count: (res.batch as IDataObject | undefined)?.duplicate,
				});
				out.push({ json: res, pairedItem: { item: i } });
				continue;
			}

			if (operation === 'getMagnetLink') {
				const infohash = String(this.getNodeParameter('infohash', i));
				const res = await call('GET', `/magnets/${encodeURIComponent(infohash)}`);
				Logger.info('Kura magnet lookup completed', { item_index: i, infohash });
				out.push({ json: res, pairedItem: { item: i } });
				continue;
			}

			if (operation === 'get') {
				const infohash = String(this.getNodeParameter('infohash', i));
				const res = await call('GET', `/releases/${encodeURIComponent(infohash)}`);
				Logger.info('Kura release lookup completed', { item_index: i, infohash });
				out.push({ json: res, pairedItem: { item: i } });
				continue;
			}

			const payload = submitPayload(this.getNodeParameter('body', i));
			const submitted: IDataObject[] = [];
			for (const body of payload.bodies) {
				submitted.push(await submitOne(call, body));
			}
			Logger.info('Kura submissions completed', {
				item_index: i,
				submit_count: submitted.length,
				conflict_count: submitted.filter((item) => item.ok === false).length,
			});
			out.push({
				json: { items: submitted, count: submitted.length },
				pairedItem: { item: i },
			});
		} catch (error) {
			const meta = {
				resource,
				operation,
				item_index: i,
				status_code: statusCode(error),
				err: (error as Error).message,
			};
			if (this.continueOnFail()) {
				Logger.warn('Kura indexer item failed', meta);
				out.push({ json: { error: (error as Error).message }, pairedItem: { item: i } });
				continue;
			}
			Logger.debug('Kura indexer item failed', meta);
			throw error;
		}
	}
	return [out];
}

function shouldErrorOnNotFound(ctx: IExecuteFunctions, itemIndex: number): boolean {
	return ctx.getNodeParameter('errorOnNotFound', itemIndex, true) !== false;
}

export function shouldResolveNotFound(errorOnNotFound: boolean, error: unknown): boolean {
	return !errorOnNotFound && isNotFoundError(error);
}

export function isNotFoundError(error: unknown): boolean {
	if (!isObject(error)) return false;
	return (
		error.httpCode === '404' ||
		error.status === 404 ||
		error.statusCode === 404 ||
		objectField(error, 'response')?.status === 404 ||
		objectField(error, 'response')?.statusCode === 404
	);
}

export function singleResolveCandidate(result: IDataObject, metadataRef: string): IDataObject {
	const candidates = arrayField(result, 'candidates');
	if (candidates.length !== 1) {
		throw new Error(`resolve returned ${candidates.length} candidates for metadata ref ${metadataRef}`);
	}
	return candidates[0];
}

type HTTPCall = (method: IHttpRequestMethods, path: string, body?: JsonObject) => Promise<IDataObject>;

type IndexerCall = (method: IHttpRequestMethods, path: string, body?: IDataObject) => Promise<IDataObject>;

function callFactory(ctx: IExecuteFunctions, credentials: IDataObject): HTTPCall {
	const baseUrl = String(credentials.baseUrl).replace(/\/+$/, '');
	const bearerToken = String(credentials.bearerToken ?? '');
	const headers = bearerToken === '' ? undefined : { Authorization: `Bearer ${bearerToken}` };
	return (method, path, body) =>
		ctx.helpers.httpRequest({
			method,
			url: `${baseUrl}${path}`,
			headers,
			body,
			json: true,
		}) as Promise<IDataObject>;
}

async function listAll(call: HTTPCall, statuses: string[], airing: string, tags: string): Promise<IDataObject[]> {
	const rows: IDataObject[] = [];
	let cursor = '';
	do {
		const query = new URLSearchParams();
		const effectiveStatuses = statuses.length === 0 ? ['complete', 'incomplete'] : statuses;
		for (const status of effectiveStatuses) query.append('status', status);
		if (airing === 'airing') query.set('airing', 'true');
		if (airing === 'notAiring') query.set('airing', 'false');
		if (tags.trim() !== '') query.set('tags', tags);
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
		tags: stringArrayField(row, 'tags'),
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

export function splitTagExpressions(value: string): string[] {
	return value.trim() === '' ? [] : value.trim().split(/\s+/);
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
	out.tags = stringArrayField(show, 'tags');
	out.seasons = filteredSeasons(show, includeSpecials).map(projectSeason);
	return out;
}

function withSpecialsFilter(show: IDataObject, includeSpecials: boolean): IDataObject {
	const out: IDataObject = {
		...show,
		tags: stringArrayField(show, 'tags'),
	};
	if (!includeSpecials) out.seasons = filteredSeasons(show, false);
	return out;
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

async function submitOne(call: IndexerCall, body: IDataObject): Promise<IDataObject> {
	try {
		await call('POST', '/submit', body);
		return { infohash: submitInfohash(body), metadataRef: submitMetadataRef(body), ok: true };
	} catch (error) {
		if (statusCode(error) === 409) {
			return { infohash: submitInfohash(body), metadataRef: submitMetadataRef(body), ok: false, error: 'conflict' };
		}
		throw error;
	}
}

function submitPayload(input: unknown): { bodies: IDataObject[] } {
	if (typeof input === 'string') {
		return submitPayload(JSON.parse(input));
	}
	if (Array.isArray(input)) {
		return { bodies: input.map(asSubmitObject) };
	}
	const body = asSubmitObject(input);
	const items = body.items ?? outputItems(body);
	if (Array.isArray(items)) {
		return { bodies: items.map(asSubmitObject) };
	}
	return { bodies: [body] };
}

function asSubmitObject(input: unknown): IDataObject {
	if (input && typeof input === 'object' && !Array.isArray(input)) {
		return input as IDataObject;
	}
	throw new Error('Submit body must be an object, an array of objects, or an object with items');
}

function outputItems(body: IDataObject): unknown {
	const output = body.output;
	if (output && typeof output === 'object' && !Array.isArray(output)) {
		return (output as IDataObject).items;
	}
	return undefined;
}

function submitInfohash(body: IDataObject): string {
	const infohash = body.infohash;
	return typeof infohash === 'string' ? infohash : '';
}

function submitMetadataRef(body: IDataObject): string {
	const ref = body.ref;
	return typeof ref === 'string' ? ref : '';
}

function statusCode(error: unknown): number | undefined {
	if (!error || typeof error !== 'object') return undefined;
	const err = error as IDataObject;
	const response = err.response;
	const nested = response && typeof response === 'object' && !Array.isArray(response) ? (response as IDataObject) : {};
	for (const value of [err.statusCode, err.httpCode, nested.statusCode, nested.status]) {
		if (typeof value === 'number') return value;
		if (typeof value === 'string') {
			const parsed = Number.parseInt(value, 10);
			if (!Number.isNaN(parsed)) return parsed;
		}
	}
	return undefined;
}

function arrayField(obj: IDataObject, key: string): IDataObject[] {
	const value = obj[key];
	return Array.isArray(value) ? (value.filter(isObject) as IDataObject[]) : [];
}

function stringField(obj: IDataObject, key: string): string {
	const value = obj[key];
	return typeof value === 'string' ? value : '';
}

function stringArrayField(obj: IDataObject, key: string): string[] {
	const value = obj[key];
	return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
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

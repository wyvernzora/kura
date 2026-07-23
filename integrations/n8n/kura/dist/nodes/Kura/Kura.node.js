"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.Kura = void 0;
exports.shouldResolveNotFound = shouldResolveNotFound;
exports.isNotFoundError = isNotFoundError;
exports.singleResolveCandidate = singleResolveCandidate;
exports.projectRow = projectRow;
exports.splitTagExpressions = splitTagExpressions;
exports.projectShow = projectShow;
const CRED = 'kuraApi';
const PAGE_LIMIT = 1000;
const ACTIONABLE_STATUSES = new Set(['complete', 'incomplete']);
class Kura {
    description = {
        displayName: 'Kura',
        name: 'kura',
        group: ['transform'],
        version: 1,
        icon: 'file:kura.svg',
        subtitle: '={{$parameter["operation"] + " (" + $parameter["resource"] + ")"}}',
        description: 'Read Kura library state and update series workflow tags',
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
            alias: ['Anime library', 'Plex library', 'Media library'],
        },
        defaults: { name: 'Kura' },
        inputs: ['main'],
        outputs: '={{$parameter["operation"] === "show" && $parameter["errorOnNotFound"] === false ? ["main", "main"] : ["main"]}}',
        outputNames: ['tracked', 'untracked'],
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
                    { name: 'Update Tags', value: 'updateTags', action: 'Update tags on a series' },
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
        ],
    };
    async execute() {
        const operation = this.getNodeParameter('operation', 0);
        const credentials = await this.getCredentials(CRED);
        const call = callFactory(this, credentials);
        if (operation === 'list') {
            const statuses = this.getNodeParameter('statuses', 0);
            const airing = this.getNodeParameter('airing', 0);
            const simplifyOutput = this.getNodeParameter('simplifyOutput', 0);
            const tags = this.getNodeParameter('tags', 0);
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
            const out = [];
            for (let i = 0; i < items.length; i++) {
                const metadataRef = this.getNodeParameter('metadataRef', i);
                const tags = splitTagExpressions(this.getNodeParameter('tagChanges', i));
                try {
                    if (tags.length === 0)
                        throw new Error('at least one tag expression is required');
                    const result = await call('PATCH', `/api/v1/series/${encodeURIComponent(metadataRef)}/tags`, { tags });
                    out.push({ json: result, pairedItem: { item: i } });
                }
                catch (error) {
                    if (this.continueOnFail()) {
                        out.push({ json: { error: error.message }, pairedItem: { item: i } });
                        continue;
                    }
                    throw error;
                }
            }
            return [out];
        }
        const items = this.getInputData();
        const out = [];
        const resolvedNotFound = [];
        let emitUntrackedOutput = false;
        for (let i = 0; i < items.length; i++) {
            const errorOnNotFound = shouldErrorOnNotFound(this, i);
            if (!errorOnNotFound)
                emitUntrackedOutput = true;
            const metadataRef = this.getNodeParameter('metadataRef', i);
            try {
                const includeSpecials = this.getNodeParameter('includeSpecials', i);
                const simplifyOutput = this.getNodeParameter('simplifyOutput', i);
                const result = await call('GET', showPath(metadataRef, queryFor(this, i)));
                out.push({ json: projectShow(result, includeSpecials, simplifyOutput), pairedItem: { item: i } });
            }
            catch (error) {
                if (shouldResolveNotFound(errorOnNotFound, error)) {
                    const result = await call('POST', '/api/v1/resolve', { terms: [metadataRef] });
                    resolvedNotFound.push({
                        json: singleResolveCandidate(result, metadataRef),
                        pairedItem: { item: i },
                    });
                    continue;
                }
                if (this.continueOnFail()) {
                    out.push({ json: { error: error.message }, pairedItem: { item: i } });
                    continue;
                }
                throw error;
            }
        }
        return emitUntrackedOutput ? [out, resolvedNotFound] : [out];
    }
}
exports.Kura = Kura;
function shouldErrorOnNotFound(ctx, itemIndex) {
    return ctx.getNodeParameter('errorOnNotFound', itemIndex, true) !== false;
}
function shouldResolveNotFound(errorOnNotFound, error) {
    return !errorOnNotFound && isNotFoundError(error);
}
function isNotFoundError(error) {
    if (!isObject(error))
        return false;
    return (error.httpCode === '404' ||
        error.status === 404 ||
        error.statusCode === 404 ||
        objectField(error, 'response')?.status === 404 ||
        objectField(error, 'response')?.statusCode === 404);
}
function singleResolveCandidate(result, metadataRef) {
    const candidates = arrayField(result, 'candidates');
    if (candidates.length !== 1) {
        throw new Error(`resolve returned ${candidates.length} candidates for metadata ref ${metadataRef}`);
    }
    return candidates[0];
}
function callFactory(ctx, credentials) {
    const baseUrl = String(credentials.baseUrl).replace(/\/+$/, '');
    const bearerToken = String(credentials.bearerToken ?? '');
    const headers = bearerToken === '' ? undefined : { Authorization: `Bearer ${bearerToken}` };
    return (method, path, body) => ctx.helpers.httpRequest({
        method,
        url: `${baseUrl}${path}`,
        headers,
        body,
        json: true,
    });
}
async function listAll(call, statuses, airing, tags) {
    const rows = [];
    let cursor = '';
    do {
        const query = new URLSearchParams();
        const effectiveStatuses = statuses.length === 0 ? ['complete', 'incomplete'] : statuses;
        for (const status of effectiveStatuses)
            query.append('status', status);
        if (airing === 'airing')
            query.set('airing', 'true');
        if (airing === 'notAiring')
            query.set('airing', 'false');
        if (tags.trim() !== '')
            query.set('tags', tags);
        query.set('limit', String(PAGE_LIMIT));
        if (cursor !== '')
            query.set('cursor', cursor);
        const result = await call('GET', `/api/v1/series?${query.toString()}`);
        rows.push(...arrayField(result, 'rows'));
        cursor = stringField(result, 'nextCursor');
    } while (cursor !== '');
    return rows;
}
function projectRow(row, simplifyOutput = true) {
    if (!simplifyOutput)
        return row;
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
function queryFor(ctx, itemIndex) {
    const query = new URLSearchParams();
    const episodes = ctx.getNodeParameter('episodes', itemIndex);
    if (episodes !== '')
        query.set('episodes', episodes);
    for (const status of ctx.getNodeParameter('status', itemIndex)) {
        query.append('status', status);
    }
    for (const source of csv(ctx.getNodeParameter('source', itemIndex))) {
        query.append('source', source);
    }
    for (const resolution of csv(ctx.getNodeParameter('resolution', itemIndex))) {
        query.append('resolution', resolution);
    }
    return query;
}
function showPath(metadataRef, query) {
    const suffix = query.toString();
    const path = `/api/v1/series/${encodeURIComponent(metadataRef)}`;
    return suffix === '' ? path : `${path}?${suffix}`;
}
function csv(value) {
    return value
        .split(',')
        .map((part) => part.trim())
        .filter((part) => part !== '');
}
function splitTagExpressions(value) {
    return value.trim() === '' ? [] : value.trim().split(/\s+/);
}
function projectShow(show, includeSpecials = false, simplifyOutput = true) {
    if (!simplifyOutput)
        return withSpecialsFilter(show, includeSpecials);
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
function withSpecialsFilter(show, includeSpecials) {
    const out = {
        ...show,
        tags: stringArrayField(show, 'tags'),
    };
    if (!includeSpecials)
        out.seasons = filteredSeasons(show, false);
    return out;
}
function filteredSeasons(show, includeSpecials) {
    return arrayField(show, 'seasons').filter((season) => includeSpecials || season.number !== 0);
}
function projectSeason(season) {
    return dropEmpty({
        number: season.number,
        episodes: arrayField(season, 'episodes').map(projectEpisode),
    });
}
function projectEpisode(episode) {
    return dropEmpty({
        episode: episode.episode,
        aired: episode.aired,
        status: episode.status,
        activeMediaFile: projectMediaFile(objectField(episode, 'active')),
        stagedMediaFile: projectMediaFile(objectField(episode, 'staged')),
    });
}
function projectMediaFile(media) {
    if (!media)
        return undefined;
    return dropEmpty({
        resolution: media.resolution,
        source: media.source,
        codec: media.codec,
        attrs: media.attrs,
        hasSubtitles: hasSubtitles(media),
    });
}
function hasSubtitles(media) {
    return arrayField(media, 'companions').some((companion) => {
        const role = stringField(companion, 'role').toLowerCase();
        const path = stringField(companion, 'path').toLowerCase();
        return (role === 'subtitle' ||
            path.endsWith('.ass') ||
            path.endsWith('.srt') ||
            path.endsWith('.ssa') ||
            path.endsWith('.vtt'));
    });
}
function arrayField(obj, key) {
    const value = obj[key];
    return Array.isArray(value) ? value.filter(isObject) : [];
}
function stringField(obj, key) {
    const value = obj[key];
    return typeof value === 'string' ? value : '';
}
function stringArrayField(obj, key) {
    const value = obj[key];
    return Array.isArray(value) ? value.filter((item) => typeof item === 'string') : [];
}
function objectField(obj, key) {
    const value = obj[key];
    return isObject(value) ? value : undefined;
}
function isObject(value) {
    return value !== null && typeof value === 'object' && !Array.isArray(value);
}
function dropEmpty(obj) {
    const out = {};
    for (const [key, value] of Object.entries(obj)) {
        if (value === undefined || value === null || value === '')
            continue;
        if (Array.isArray(value) && value.length === 0)
            continue;
        out[key] = value;
    }
    return out;
}
//# sourceMappingURL=Kura.node.js.map
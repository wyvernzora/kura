import type {
	Icon,
	ICredentialTestRequest,
	ICredentialType,
	INodeProperties,
} from 'n8n-workflow';

/**
 * Credentials for the Kura release indexer (REST catalog, ingest, and queue
 * surfaces). The indexer enforces no application-level auth by design, so
 * this is just a base URL.
 */
export class KuraReleasesApi implements ICredentialType {
	name = 'kuraReleasesApi';
	displayName = 'Kura Release Indexer API';
	documentationUrl = 'https://github.com/wyvernzora/kura';
	icon: Icon = 'file:kura.svg';

	properties: INodeProperties[] = [
		{
			displayName: 'Base URL',
			name: 'baseUrl',
			type: 'string',
			default: 'http://kura-release-indexer:8080',
			placeholder: 'http://kura-release-indexer:8080',
			required: true,
			description: 'Base URL of the Kura release-indexer service',
		},
	];

	test: ICredentialTestRequest = {
		request: {
			baseURL: '={{$credentials.baseUrl}}',
			url: '/healthz',
			method: 'GET',
		},
	};
}

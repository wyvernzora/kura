import type {
	ICredentialTestRequest,
	ICredentialType,
	INodeProperties,
} from 'n8n-workflow';

export class KuraApi implements ICredentialType {
	name = 'kuraApi';
	displayName = 'Kura API';
	documentationUrl = 'https://github.com/wyvernzora/kura';

	properties: INodeProperties[] = [
		{
			displayName: 'Base URL',
			name: 'baseUrl',
			type: 'string',
			default: 'http://kura:8080',
			placeholder: 'http://kura:8080',
			required: true,
			description: 'Base URL of the Kura REST service',
		},
		{
			displayName: 'Bearer Token',
			name: 'bearerToken',
			type: 'string',
			typeOptions: { password: true },
			default: '',
			description: 'Optional KURA_TOKEN value sent as an Authorization bearer token',
		},
	];

	test: ICredentialTestRequest = {
		request: {
			baseURL: '={{$credentials.baseUrl}}',
			url: '/api/v1/health',
			method: 'GET',
			headers: {
				Authorization: '={{$credentials.bearerToken ? "Bearer " + $credentials.bearerToken : ""}}',
			},
		},
	};
}

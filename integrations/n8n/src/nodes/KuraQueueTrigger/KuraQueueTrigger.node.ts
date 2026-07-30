import type {
	IDataObject,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
	IPollFunctions,
	JsonObject,
} from 'n8n-workflow';
import { LoggerProxy as Logger, NodeApiError, NodeConnectionTypes } from 'n8n-workflow';

const CRED = 'kuraApi';

/**
 * Kura Queue Trigger — polls by claiming release-indexer queue work. It stays
 * idle when no work is claimable and emits one batch item when work exists.
 * The poll schedule comes from n8n's standard polling UI.
 */
export class KuraQueueTrigger implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'Kura Queue Trigger',
		name: 'kuraQueueTrigger',
		icon: {
			light: 'file:../../assets/kura.svg',
			dark: 'file:../../assets/kura.dark.svg',
		},
		group: ['trigger'],
		version: 2,
		subtitle: '={{"claim: " + $parameter["limit"]}}',
		description: 'Claims Kura release-indexer queue work when releases are available',
		codex: {
			categories: ['Core Nodes'],
			subcategories: {
				'Core Nodes': ['Helpers'],
			},
		},
		defaults: { name: 'Kura Queue Trigger' },
		polling: true,
		inputs: [],
		outputs: [NodeConnectionTypes.Main],
		usableAsTool: true,
		credentials: [{ name: CRED, required: true }],
		properties: [
			{
				displayName: 'Limit',
				name: 'limit',
				type: 'number',
				typeOptions: { minValue: 1 },
				default: 10,
				description: 'Max releases to claim per poll',
			},
			{
				displayName: 'Lease Seconds',
				name: 'leaseSeconds',
				type: 'number',
				default: 300,
				description: 'Lease length; honored if supplied, else a server default',
			},
		],
	};

	async poll(this: IPollFunctions): Promise<INodeExecutionData[][] | null> {
		const credentials = await this.getCredentials(CRED);
		const baseUrl = String(credentials.baseUrl).replace(/\/+$/, '');

		let res: IDataObject;
		try {
			res = (await this.helpers.httpRequestWithAuthentication.call(this, CRED, {
				method: 'POST',
				url: `${baseUrl}/api/v1/releases/queue/claim`,
				body: {
					limit: Number(this.getNodeParameter('limit', 10)),
					leaseSeconds: Number(this.getNodeParameter('leaseSeconds', 300)),
				},
				json: true,
			})) as IDataObject;
		} catch (error) {
			Logger.debug('Kura queue trigger claim failed', { err: (error as Error).message });
			throw new NodeApiError(this.getNode(), error as JsonObject);
		}

		const claimed = (res.items as IDataObject[]) ?? [];
		if (claimed.length === 0) {
			Logger.debug('Kura queue trigger poll completed with no claims');
			return null;
		}

		Logger.info('Kura queue trigger emitted claims', { claimed_count: claimed.length });
		return [[{ json: { items: claimed, count: claimed.length } }]];
	}
}

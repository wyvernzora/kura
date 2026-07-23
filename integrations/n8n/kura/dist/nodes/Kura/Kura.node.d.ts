import type { IDataObject, IExecuteFunctions, INodeExecutionData, INodeType, INodeTypeDescription } from 'n8n-workflow';
export declare class Kura implements INodeType {
    description: INodeTypeDescription;
    execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]>;
}
export declare function shouldResolveNotFound(errorOnNotFound: boolean, error: unknown): boolean;
export declare function isNotFoundError(error: unknown): boolean;
export declare function singleResolveCandidate(result: IDataObject, metadataRef: string): IDataObject;
export declare function projectRow(row: IDataObject, simplifyOutput?: boolean): IDataObject;
export declare function splitTagExpressions(value: string): string[];
export declare function projectShow(show: IDataObject, includeSpecials?: boolean, simplifyOutput?: boolean): IDataObject;

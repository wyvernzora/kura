import { cn } from '@/lib/cn';

/**
 * Shimmer placeholder for the series detail layout. Renders a
 * poster-shaped block on the left and stacked row stubs on the
 * right. Same overall grid as the loaded view so layout doesn't
 * jump when the data resolves.
 */
export function SeriesDetailSkeleton() {
  return (
    <div
      className={cn(
        'mx-auto grid max-w-[1920px] gap-9 px-[18px] pt-8 pb-12',
        'md:grid-cols-[300px_1fr]',
      )}
    >
      <div className="flex flex-col gap-3.5">
        <SkelBlock className="aspect-[0.7] w-full rounded-[12px]" />
        <SkelBlock className="h-3 w-24" />
        <SkelBlock className="h-7 w-3/4" />
        <SkelBlock className="h-[38px] w-full rounded-md" />
      </div>
      <div className="min-w-0">
        <SkelBlock className="mb-[18px] h-[56px] w-full rounded-[12px]" />
        {Array.from({ length: 6 }).map((_, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: skeleton stubs have no real identity
          <SkelBlock key={i} className="mb-2 h-[60px] w-full rounded-md" />
        ))}
      </div>
    </div>
  );
}

function SkelBlock({ className }: { className?: string }) {
  return <div aria-hidden="true" className={cn('animate-pulse bg-line-soft', className)} />;
}

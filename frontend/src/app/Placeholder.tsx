export function Placeholder({ title, soon }: { title: string; soon: string }) {
  return (
    <section>
      <h1>{title}</h1>
      <p>Экран появится: {soon}.</p>
    </section>
  );
}

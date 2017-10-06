import { Component } from '@angular/core';
import * as _ from "lodash";

// Services
import { ChartsService } from '../charts.service';

@Component({
    selector: 'throughput-chart',
    templateUrl: './throughput-chart.component.html',
    providers: [ChartsService],
})

export class ThroughtputChartComponent {

    throughputChartObject: any;
    chartConfig: any;
    pendingThroughput: Array<any>;

    constructor(private chartsService: ChartsService) {

        this.chartsService.fetchConfig()
            .then( (resp: any) => {
                this.chartConfig = resp;
                this.throughputChartObject = {
                    chartType: 'LineChart',
                    data: {},
                    chart: null,
                    domElement: 'throughput-chart',
                    columns: [['Timestamp', 'datetime']].concat(_.map(this.chartConfig.Throughput, (k: string, v: string) => { return [v, 'number']; })),
                    options: Object.assign({
                        vAxis: {
                            title: 'Throughput (rps)',
                        },
                        hAxis: {
                            viewWindow: {}
                        },
                        colors: ['#FFC000', '#208DB9'],
                    }, this.chartsService.getLineChartOptions())
                };

                this.setupChart();
            });
    }

    setupChart() {
        let script = document.createElement('script');
        script.src = '//www.google.com/jsapi';
        script.onload = () => {
            (<any>window).google.charts.load("visualization", "1", {packages: ["corechart", "gauge"]});
            (<any>window).google.charts.setOnLoadCallback(this.chartsService.drawLineChart.bind(this, this.throughputChartObject));
        };
        document.head.appendChild(script);

        this.pendingThroughput = [];

        let obsv = this.chartsService.getObservable();
        obsv.subscribe((evt:any) => {
            let data = JSON.parse(evt.data);
            if (data.MetricType === 'Throughput') {
                // console.log(data);
                this.aggregateData(data);
            }
        });

        setInterval(() => {
            _.forEach(this.pendingThroughput, (data:any) => {
                if (this.throughputChartObject.chartReady) {
                    this.chartsService.updateLineChart(data, this.throughputChartObject);
                }
            });
            this.pendingThroughput.splice(0);
        }, 1000)
    }

    /*
     * Function used to build the proper throughput metrics
     * And push the data to the pending array
     */
    aggregateData(data: any) {

        this.pendingThroughput.push(data);
        let grouped = _.groupBy(this.pendingThroughput, (evt:any) => {
            return evt.Timestamp
        });

        let combined:Array<any> = [];
        _.forEach(grouped, (items:any, time:any) => {
            let combinedItem = {
                Values: {}
            };

            _.forEach(items, (item:any) => {
                Object.assign(combinedItem, item);
                combinedItem.Values[item.Sender] = item.Value;
            });

            // Make sure we have all the columns defined or the chart goes ape
            _.forEach(this.chartConfig.Throughput, (val:any, key:any) => {
                combinedItem.Values[key] = combinedItem.Values[key] ? combinedItem.Values[key] : 0;
            });

            combined.push(combinedItem);
        });
        this.pendingThroughput = combined;
    }
}
